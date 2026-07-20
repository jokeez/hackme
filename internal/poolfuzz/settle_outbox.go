package poolfuzz

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// SettleOutboxItem is a queued fuzz escrow settlement for pull-mode origin nodes.
type SettleOutboxItem struct {
	ID           int64  `json:"id"`
	CampaignID   string `json:"campaign_id"`
	Kind         string `json:"kind"`
	MinerAddress string `json:"miner_address"`
	Severity     string `json:"severity"`
	CreatedAt    int64  `json:"created_at"`
}

// EnqueueSettleOutbox records a settlement for durable event-id based apply/pull.
// Returns the outbox row id used as SettleEventID / chain.FuzzSettleEventID.
// Replay-safe: same (campaign_id, kind, miner_address, severity) returns the existing row id.
func (s *Service) EnqueueSettleOutbox(ctx context.Context, kind, campaignID, minerAddress, severity string) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("poolfuzz: no database")
	}
	kind = strings.TrimSpace(strings.ToLower(kind))
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" || kind == "" {
		return 0, fmt.Errorf("poolfuzz: settle outbox missing kind or campaign_id")
	}
	minerAddress = strings.TrimSpace(minerAddress)
	severity = strings.TrimSpace(severity)
	var existing int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT id FROM fuzz_settle_outbox
		 WHERE campaign_id=? AND kind=? AND miner_address=? AND severity=?
		 ORDER BY id ASC LIMIT 1`,
		campaignID, kind, minerAddress, severity).Scan(&existing)
	if err == nil && existing > 0 {
		return existing, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	now := time.Now().Unix()
	res, err := s.DB.ExecContext(ctx,
		`INSERT INTO fuzz_settle_outbox (campaign_id, kind, miner_address, severity, status, created_at)
		 VALUES (?, ?, ?, ?, 'pending', ?)`,
		campaignID, kind, minerAddress, severity, now)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// ListPendingSettleOutbox returns pending outbox rows oldest-first.
func (s *Service) ListPendingSettleOutbox(ctx context.Context, limit int) ([]SettleOutboxItem, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("poolfuzz: no database")
	}
	if limit <= 0 || limit > 500 {
		limit = 64
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, campaign_id, kind, miner_address, severity, created_at
		 FROM fuzz_settle_outbox
		 WHERE status='pending'
		 ORDER BY id ASC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SettleOutboxItem, 0, limit)
	for rows.Next() {
		var it SettleOutboxItem
		if err := rows.Scan(&it.ID, &it.CampaignID, &it.Kind, &it.MinerAddress, &it.Severity, &it.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// AckSettleOutbox marks outbox rows applied by the origin node.
func (s *Service) AckSettleOutbox(ctx context.Context, ids []int64) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("poolfuzz: no database")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	now := time.Now().Unix()
	var n int64
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		res, err := s.DB.ExecContext(ctx,
			`UPDATE fuzz_settle_outbox SET status='applied', applied_at=? WHERE id=? AND status='pending'`,
			now, id)
		if err != nil {
			return n, err
		}
		aff, _ := res.RowsAffected()
		n += aff
		// Promote local work-item settle status from queued → paid once origin ACKed.
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items SET settle_run_status='paid'
			 WHERE settle_run_outbox_id=? AND settle_run_status IN ('queued','pending')`, id)
		_, _ = s.DB.ExecContext(ctx,
			`UPDATE fuzz_work_items SET settle_finding_status='paid'
			 WHERE settle_finding_outbox_id=? AND settle_finding_status IN ('queued','pending')`, id)
	}
	return n, nil
}

// SettleOutboxStatus returns pending|applied|"" for an outbox row id.
func (s *Service) SettleOutboxStatus(ctx context.Context, id int64) (string, error) {
	if s == nil || s.DB == nil {
		return "", fmt.Errorf("poolfuzz: no database")
	}
	if id <= 0 {
		return "", nil
	}
	var st string
	err := s.DB.QueryRowContext(ctx,
		`SELECT status FROM fuzz_settle_outbox WHERE id=?`, id).Scan(&st)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.ToLower(st)), nil
}

// CampaignConfig loads parsed config_json for a pool campaign.
func (s *Service) CampaignConfig(ctx context.Context, campaignID string) (map[string]any, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("poolfuzz: no database")
	}
	campaignID = strings.TrimSpace(campaignID)
	var raw string
	err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&raw)
	if err == sql.ErrNoRows {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return parseConfigJSON(raw), nil
}
