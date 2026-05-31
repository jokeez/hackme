package hms

import (
	"crypto/sha256"
	"errors"
	"strconv"
	"time"
)

// Warm (keep-alive) mining — ASICs always receive Stratum work between seal windows.
const (
	WorkModeWarm = "warm"
	WorkModeSeal = "seal"

	WarmJobRotateSec      = int64(300)
	WarmTHPerSubmit       = 4294967296.0 / 1e12 // ~0.00429 TH per Stratum submit
	WarmShareAccrualUnits = 500                 // 0.000005 HMS per accepted warm share
	WarmHMSPerTHPerHour   = 0.000001
)

func warmManifestRoot(slot int64) [32]byte {
	return sha256.Sum256([]byte("hms-warm-manifest|hackme-official|" + strconv.FormatInt(slot, 10)))
}

func warmTarget() []byte {
	t := make([]byte, 32)
	t[0] = 0x00
	t[1] = 0x00
	t[2] = 0xff
	t[3] = 0xff
	return t
}

func currentWarmSlot(now int64) int64 {
	if now <= 0 {
		now = time.Now().Unix()
	}
	return now / WarmJobRotateSec
}

func warmWorkPackage(poolID string, slot int64) map[string]any {
	root := warmManifestRoot(slot)
	return map[string]any{
		"work_mode":     WorkModeWarm,
		"warm_slot":     slot,
		"epoch_id":      int64(0),
		"manifest_root": encodeHex(root[:]),
		"target":        encodeHex(warmTarget()),
		"pool_id":       poolID,
	}
}

// StratumWork returns seal work during the seal window, otherwise warm keep-alive work.
func (c *Coordinator) StratumWork() (map[string]any, error) {
	ep, err := c.CurrentEpoch()
	if err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	if now >= ep.FreezeUnix && now < ep.SealEndUnix && ep.Sealed == 0 {
		if len(ep.ManifestRoot) == 0 {
			c.mu.Lock()
			root, berr := c.buildManifestLocked(ep.EpochID)
			c.mu.Unlock()
			if berr != nil {
				return warmWorkPackage(c.cfg.PoolID, currentWarmSlot(now)), nil
			}
			ep.ManifestRoot = root[:]
			_, _ = c.db.Exec(`UPDATE hms_epochs SET manifest_root=? WHERE epoch_id=?`, root[:], ep.EpochID)
		}
		if len(ep.ManifestRoot) > 0 {
			var root [32]byte
			copy(root[:], ep.ManifestRoot)
			return map[string]any{
				"work_mode":     WorkModeSeal,
				"epoch_id":      ep.EpochID,
				"manifest_root": encodeHex(root[:]),
				"target":        encodeHex(ep.SealTarget),
				"pool_id":       c.cfg.PoolID,
				"seal_end_unix": ep.SealEndUnix,
			}, nil
		}
	}
	return warmWorkPackage(c.cfg.PoolID, currentWarmSlot(now)), nil
}

// EnsureStratumWorker registers ASIC worker name from pool settings (authorize param).
func (c *Coordinator) EnsureStratumWorker(workerID string) error {
	workerID = trimWorkerID(workerID)
	if workerID == "" {
		return nil
	}
	now := time.Now().Unix()
	_, err := c.db.Exec(`INSERT INTO hms_workers(worker_id, role, pubkey_hex, quota_gb, last_seen_unix, created_unix)
		VALUES(?, 'seal', '', 0, ?, ?)
		ON CONFLICT(worker_id) DO UPDATE SET role='seal', last_seen_unix=excluded.last_seen_unix`,
		workerID, now, now)
	return err
}

// SubmitWarmShare validates keep-alive hash and credits warm accrual.
func (c *Coordinator) SubmitWarmShare(workerID string, slot int64, nonce uint64) error {
	workerID = trimWorkerID(workerID)
	if workerID == "" {
		return errors.New("worker_id required")
	}
	root := warmManifestRoot(slot)
	hash := SealHash(slot, root, c.cfg.PoolID, nonce)
	if !HashBelowTarget(hash[:], warmTarget()) {
		return errors.New("warm share below target")
	}
	_ = c.EnsureStratumWorker(workerID)
	if err := c.accrueWarmShare(workerID, WarmShareAccrualUnits); err != nil {
		return err
	}
	_ = c.RecordWarmShare(slot, workerID)
	return nil
}

func (c *Coordinator) accrueWarmShare(workerID string, units uint64) error {
	return c.addWarmAccrual(workerID, units, true)
}

func (c *Coordinator) addWarmAccrual(workerID string, units uint64, countShare bool) error {
	if units == 0 {
		return nil
	}
	now := time.Now().Unix()
	if countShare {
		_, err := c.db.Exec(`INSERT INTO hms_warm_accrual(worker_id, accrual_units, shares_total, updated_unix)
			VALUES(?, ?, 1, ?)
			ON CONFLICT(worker_id) DO UPDATE SET
				accrual_units = accrual_units + excluded.accrual_units,
				shares_total = shares_total + 1,
				updated_unix = excluded.updated_unix`,
			workerID, units, now)
		return err
	}
	_, err := c.db.Exec(`INSERT INTO hms_warm_accrual(worker_id, accrual_units, shares_total, updated_unix)
		VALUES(?, ?, 0, ?)
		ON CONFLICT(worker_id) DO UPDATE SET
			accrual_units = accrual_units + excluded.accrual_units,
			updated_unix = excluded.updated_unix`,
		workerID, units, now)
	return err
}

func (c *Coordinator) RecordWarmShare(slot int64, workerID string) error {
	_, err := c.db.Exec(`INSERT INTO hms_seal_shares(epoch_id, worker_id, shares_ok, shares_fail)
		VALUES(?, ?, 1, 0)
		ON CONFLICT(epoch_id, worker_id) DO UPDATE SET shares_ok = shares_ok + 1`,
		-warmShareEpochKey(slot), workerID)
	return err
}

func warmShareEpochKey(slot int64) int64 {
	return slot
}

func (c *Coordinator) warmAccrualUnits(workerID string) uint64 {
	var u uint64
	_ = c.db.QueryRow(`SELECT accrual_units FROM hms_warm_accrual WHERE worker_id=?`, workerID).Scan(&u)
	return u
}

func (c *Coordinator) tickWarmTimeAccrual(elapsedSec float64) {
	if c.stratum == nil || elapsedSec <= 0 {
		return
	}
	for workerID, th := range c.stratum.EffectiveTHByWorker() {
		if th <= 0 {
			continue
		}
		hms := th * WarmHMSPerTHPerHour * (elapsedSec / 3600.0)
		units := hmsToUnits(hms)
		if units == 0 {
			continue
		}
		_ = c.addWarmAccrual(workerID, units, false)
	}
}

func workModeFromPackage(work map[string]any) string {
	if work == nil {
		return WorkModeWarm
	}
	if m, _ := work["work_mode"].(string); m != "" {
		return m
	}
	return WorkModeWarm
}

func warmSlotFromWork(work map[string]any) int64 {
	if work == nil {
		return currentWarmSlot(0)
	}
	switch x := work["warm_slot"].(type) {
	case int64:
		return x
	case float64:
		return int64(x)
	case int:
		return int64(x)
	default:
		return currentWarmSlot(0)
	}
}

func formatHashrateSummary(totalTH float64) map[string]any {
	out := map[string]any{
		"seal_hashrate_th": totalTH,
		"seal_hashrate":    totalTH,
	}
	if totalTH >= 1000 {
		out["seal_hashrate_ph"] = totalTH / 1000.0
	} else {
		out["seal_hashrate_ph"] = 0.0
	}
	return out
}
