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
	WorkItemID   int64  `json:"work_item_id,omitempty"`
	CreatedAt    int64  `json:"created_at"`
}

// ensureSettleOutboxSchema adds work_item_id + UNIQUE so concurrent enqueues cannot
// double-insert and distinct work items cannot collapse into one underpaid event_id.
func ensureSettleOutboxSchema(db *sql.DB) {
	if db == nil {
		return
	}
	_, _ = db.Exec(`ALTER TABLE fuzz_settle_outbox ADD COLUMN work_item_id INTEGER NOT NULL DEFAULT 0`)
	// Collapse legacy duplicates (same logical key) before UNIQUE.
	_, _ = db.Exec(`
		DELETE FROM fuzz_settle_outbox
		 WHERE id NOT IN (
		   SELECT MIN(id) FROM fuzz_settle_outbox
		    GROUP BY campaign_id, kind, miner_address, severity, COALESCE(work_item_id,0)
		 )`)
	_, _ = db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_fuzz_settle_outbox_uq
		 ON fuzz_settle_outbox(campaign_id, kind, miner_address, severity, work_item_id)`)
}

// EnqueueSettleOutbox records a settlement for durable event-id based apply/pull.
// Returns the outbox row id; durable event_id is SettleEventID(campaignID, id)
// / chain.FuzzSettleEventID(campaignID, id) → outbox:<campaign_id>:<id>.
// Replay-safe: same (campaign_id, kind, miner_address, severity, work_item_id) returns the existing row id.
// workItemID distinguishes per-run pays (FUZZ-01); use 0 for campaign-level finding/finalize.
func (s *Service) EnqueueSettleOutbox(ctx context.Context, kind, campaignID, minerAddress, severity string, workItemID int64) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("poolfuzz: no database")
	}
	ensureSettleOutboxSchema(s.DB)
	kind = strings.TrimSpace(strings.ToLower(kind))
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" || kind == "" {
		return 0, fmt.Errorf("poolfuzz: settle outbox missing kind or campaign_id")
	}
	minerAddress = strings.TrimSpace(minerAddress)
	severity = strings.TrimSpace(severity)
	if workItemID < 0 {
		workItemID = 0
	}

	// Prefer an immediate write lock so check-then-insert cannot race under load.
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		return 0, err
	}
	committed := false
	defer func() {
		if !committed {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
		}
	}()

	var existing int64
	err = conn.QueryRowContext(ctx,
		`SELECT id FROM fuzz_settle_outbox
		 WHERE campaign_id=? AND kind=? AND miner_address=? AND severity=? AND work_item_id=?
		 ORDER BY id ASC LIMIT 1`,
		campaignID, kind, minerAddress, severity, workItemID).Scan(&existing)
	if err == nil && existing > 0 {
		if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
			return 0, err
		}
		committed = true
		return existing, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return 0, err
	}
	now := time.Now().Unix()
	res, err := conn.ExecContext(ctx,
		`INSERT INTO fuzz_settle_outbox (campaign_id, kind, miner_address, severity, status, created_at, work_item_id)
		 VALUES (?, ?, ?, ?, 'pending', ?, ?)`,
		campaignID, kind, minerAddress, severity, now, workItemID)
	if err != nil {
		// UNIQUE race: another writer won — return their row.
		var raced int64
		if qerr := conn.QueryRowContext(ctx,
			`SELECT id FROM fuzz_settle_outbox
			 WHERE campaign_id=? AND kind=? AND miner_address=? AND severity=? AND work_item_id=?
			 ORDER BY id ASC LIMIT 1`,
			campaignID, kind, minerAddress, severity, workItemID).Scan(&raced); qerr == nil && raced > 0 {
			if _, cerr := conn.ExecContext(ctx, `COMMIT`); cerr == nil {
				committed = true
			}
			return raced, nil
		}
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := conn.ExecContext(ctx, `COMMIT`); err != nil {
		return 0, err
	}
	committed = true
	return id, nil
}

// ListPendingSettleOutbox returns pending outbox rows oldest-first.
func (s *Service) ListPendingSettleOutbox(ctx context.Context, limit int) ([]SettleOutboxItem, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("poolfuzz: no database")
	}
	ensureSettleOutboxSchema(s.DB)
	if limit <= 0 || limit > 500 {
		limit = 64
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, campaign_id, kind, miner_address, severity, created_at, COALESCE(work_item_id,0)
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
		if err := rows.Scan(&it.ID, &it.CampaignID, &it.Kind, &it.MinerAddress, &it.Severity, &it.CreatedAt, &it.WorkItemID); err != nil {
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
