package hms

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// RecordSealShare persists Stratum/HTTP seal attempt counters for the participation pool.
func (c *Coordinator) RecordSealShare(epochID int64, workerID string, ok bool) error {
	workerID = trimWorkerID(workerID)
	if epochID <= 0 || workerID == "" {
		return nil
	}
	col := "shares_fail"
	if ok {
		col = "shares_ok"
	}
	q := fmt.Sprintf(`INSERT INTO hms_seal_shares(epoch_id, worker_id, shares_ok, shares_fail)
		VALUES(?, ?, ?, ?)
		ON CONFLICT(epoch_id, worker_id) DO UPDATE SET %s = %s + 1`, col, col)
	var okInc, failInc int
	if ok {
		okInc = 1
	} else {
		failInc = 1
	}
	_, err := c.db.Exec(q, epochID, workerID, okInc, failInc)
	return err
}

// FlushStratumShares merges in-memory Stratum peer counters into hms_seal_shares for an epoch.
func (c *Coordinator) FlushStratumShares(epochID int64) error {
	if c.stratum == nil || epochID <= 0 {
		return nil
	}
	for workerID, counts := range c.stratum.SharesByWorker() {
		if counts[0] == 0 && counts[1] == 0 {
			continue
		}
		_, err := c.db.Exec(`INSERT INTO hms_seal_shares(epoch_id, worker_id, shares_ok, shares_fail)
			VALUES(?, ?, ?, ?)
			ON CONFLICT(epoch_id, worker_id) DO UPDATE SET
				shares_ok = shares_ok + excluded.shares_ok,
				shares_fail = shares_fail + excluded.shares_fail`,
			epochID, workerID, counts[0], counts[1])
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) epochPrepaidUnits(epochID int64) (uint64, error) {
	var sum sql.NullFloat64
	err := c.db.QueryRow(`
		SELECT COALESCE(SUM(ROUND(o.prepaid_hmc * 100000000)), 0)
		FROM hms_orders o
		WHERE o.order_id IN (
			SELECT DISTINCT oc.order_id
			FROM hms_order_chunks oc
			INNER JOIN hms_chunks ch ON ch.chunk_id = oc.chunk_id
			WHERE ch.epoch_id = ?
		) AND o.prepaid_hmc > 0`, epochID).Scan(&sum)
	if err != nil {
		return 0, err
	}
	if sum.Valid && sum.Float64 > 0 {
		return uint64(sum.Float64), nil
	}
	return 0, nil
}

// epochPrepaidHMC kept for dashboards; seal budget uses epochPrepaidUnits.
func (c *Coordinator) epochPrepaidHMC(epochID int64) (float64, error) {
	u, err := c.epochPrepaidUnits(epochID)
	if err != nil {
		return 0, err
	}
	return float64(u) / float64(HMSUnitsPerCoin), nil
}

func (c *Coordinator) loadSealShares(epochID int64) (map[string]uint64, error) {
	rows, err := c.db.Query(`SELECT worker_id, shares_ok FROM hms_seal_shares WHERE epoch_id=? AND shares_ok > 0`, epochID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]uint64{}
	for rows.Next() {
		var wid string
		var n uint64
		if err := rows.Scan(&wid, &n); err != nil {
			return nil, err
		}
		out[trimWorkerID(wid)] = n
	}
	return out, rows.Err()
}

// FinalizeEpochSealPayouts computes hybrid seal rewards idempotently after a seal.
func (c *Coordinator) FinalizeEpochSealPayouts(epochID int64) ([]SealPayoutLine, error) {
	var sealed int
	var winner string
	var finalized int
	err := c.db.QueryRow(`SELECT sealed, COALESCE(seal_worker_id,''), payouts_finalized FROM hms_epochs WHERE epoch_id=?`,
		epochID).Scan(&sealed, &winner, &finalized)
	if err != nil {
		return nil, err
	}
	if sealed == 0 {
		return nil, errors.New("epoch not sealed")
	}
	if finalized != 0 {
		return c.loadEpochPayouts(epochID)
	}

	if err := c.FlushStratumShares(epochID); err != nil {
		return nil, err
	}
	prepaidUnits, err := c.epochPrepaidUnits(epochID)
	if err != nil {
		return nil, err
	}
	budget := SealEpochBudgetFromPrepaidUnits(prepaidUnits)
	shares, err := c.loadSealShares(epochID)
	if err != nil {
		return nil, err
	}
	lines, err := ComputeSealEpochPayouts(budget, winner, shares)
	if err != nil {
		return nil, err
	}

	tx, err := c.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`UPDATE hms_epochs SET seal_budget_units=?, payouts_finalized=1
		WHERE epoch_id=? AND sealed=1 AND payouts_finalized=0`, budget, epochID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		_ = tx.Rollback()
		return c.loadEpochPayouts(epochID)
	}
	for _, line := range lines {
		breakdown, _ := json.Marshal(map[string]uint64{
			"winner_units":        line.WinnerUnits,
			"participation_units": line.ParticipationUnits,
			"shares_ok":           line.SharesOK,
		})
		_, err = tx.Exec(`INSERT INTO hms_epoch_payouts(epoch_id, worker_id, winner_units, participation_units, total_units, breakdown_json, finalized_unix)
			VALUES(?, ?, ?, ?, ?, ?, strftime('%s','now'))
			ON CONFLICT(epoch_id, worker_id) DO NOTHING`,
			epochID, line.WorkerID, line.WinnerUnits, line.ParticipationUnits, line.TotalUnits, string(breakdown))
		if err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return lines, nil
}

func (c *Coordinator) loadEpochPayouts(epochID int64) ([]SealPayoutLine, error) {
	rows, err := c.db.Query(`SELECT worker_id, winner_units, participation_units, total_units, breakdown_json
		FROM hms_epoch_payouts WHERE epoch_id=? ORDER BY total_units DESC, worker_id ASC`, epochID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SealPayoutLine
	for rows.Next() {
		var line SealPayoutLine
		var breakdown string
		if err := rows.Scan(&line.WorkerID, &line.WinnerUnits, &line.ParticipationUnits, &line.TotalUnits, &breakdown); err != nil {
			return nil, err
		}
		var meta map[string]uint64
		_ = json.Unmarshal([]byte(breakdown), &meta)
		line.SharesOK = meta["shares_ok"]
		out = append(out, line)
	}
	return out, rows.Err()
}

// EpochSealSettlement returns payout summary for operators / mint scripts.
func (c *Coordinator) EpochSealSettlement(epochID int64) (map[string]any, error) {
	var sealed, finalized int
	var winner string
	var budget uint64
	err := c.db.QueryRow(`SELECT sealed, COALESCE(seal_worker_id,''), seal_budget_units, payouts_finalized
		FROM hms_epochs WHERE epoch_id=?`, epochID).Scan(&sealed, &winner, &budget, &finalized)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"epoch_id":                 epochID,
		"sealed":                   sealed == 1,
		"seal_worker_id":           winner,
		"seal_budget_units":        budget,
		"seal_budget_hms":          float64(budget) / HMSUnitsPerCoin,
		"payouts_finalized":        finalized == 1,
		"seal_reward_policy":       SealRewardPolicyHash(),
		"winner_share_rate":        SealWinnerShareRate,
		"participation_share_rate": SealParticipationShareRate,
	}
	if sealed == 0 {
		return out, nil
	}
	// Read-only: never finalize on GET/status path — finalize is POST+admin only.
	lines, err := c.loadEpochPayouts(epochID)
	if err != nil {
		return nil, err
	}
	payouts := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		payouts = append(payouts, map[string]any{
			"worker_id":           line.WorkerID,
			"winner_units":        line.WinnerUnits,
			"participation_units": line.ParticipationUnits,
			"total_units":         line.TotalUnits,
			"total_hms":           float64(line.TotalUnits) / HMSUnitsPerCoin,
			"shares_ok":           line.SharesOK,
		})
	}
	out["payouts"] = payouts
	return out, nil
}
