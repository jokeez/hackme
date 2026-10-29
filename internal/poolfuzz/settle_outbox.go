package poolfuzz

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
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

var settleOutboxSchemaOnce sync.Once

// ensureSettleOutboxSchema adds work_item_id + UNIQUE so concurrent enqueues cannot
// double-insert and distinct work items cannot collapse into one underpaid event_id.
// Must run once: the legacy duplicate DELETE scans the whole outbox and starved the
// coordinator when called on every settle poll / enqueue.
func ensureSettleOutboxSchema(db *sql.DB) {
	if db == nil {
		return
	}
	settleOutboxSchemaOnce.Do(func() {
		_, _ = db.Exec(`ALTER TABLE fuzz_settle_outbox ADD COLUMN work_item_id INTEGER NOT NULL DEFAULT 0`)
		// Collapse legacy duplicates (same logical key) before UNIQUE — one-shot only.
		_, _ = db.Exec(`
			DELETE FROM fuzz_settle_outbox
			 WHERE id NOT IN (
			   SELECT MIN(id) FROM fuzz_settle_outbox
			    GROUP BY campaign_id, kind, miner_address, severity, COALESCE(work_item_id,0)
			 )`)
		_, _ = db.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_fuzz_settle_outbox_uq
			 ON fuzz_settle_outbox(campaign_id, kind, miner_address, severity, work_item_id)`)
	})
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

// CountPendingSettleOutbox returns how many pending settle rows exist for campaignID.
// Close/finalize guards must use this — not the fair ListPending window — or a
// campaign whose rows sit outside the fair sample can look "empty" and refund early.
func (s *Service) CountPendingSettleOutbox(ctx context.Context, campaignID string) (int64, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("poolfuzz: no database")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return 0, fmt.Errorf("poolfuzz: campaign_id required")
	}
	ensureSettleOutboxSchema(s.DB)
	var n int64
	err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_settle_outbox WHERE status='pending' AND campaign_id=?`,
		campaignID).Scan(&n)
	return n, err
}

// ListPendingSettleOutboxForCampaign returns pending rows for one campaign (oldest-first).
func (s *Service) ListPendingSettleOutboxForCampaign(ctx context.Context, campaignID string, limit int) ([]SettleOutboxItem, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("poolfuzz: no database")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return nil, fmt.Errorf("poolfuzz: campaign_id required")
	}
	ensureSettleOutboxSchema(s.DB)
	if limit <= 0 || limit > 500 {
		limit = 64
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, campaign_id, kind, miner_address, severity, created_at, COALESCE(work_item_id,0)
		 FROM fuzz_settle_outbox
		 WHERE status='pending' AND campaign_id=?
		 ORDER BY CASE kind WHEN 'finalize' THEN 1 ELSE 0 END ASC, id ASC
		 LIMIT ?`, campaignID, limit)
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

// ListPendingSettleOutbox returns pending outbox rows with a fair multi-campaign
// window. A single orphan/foreign campaign must not monopolize the pull batch
// (FIFO head of 1200 runs used to block every other payer's settle-pull).
func (s *Service) ListPendingSettleOutbox(ctx context.Context, limit int) ([]SettleOutboxItem, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("poolfuzz: no database")
	}
	ensureSettleOutboxSchema(s.DB)
	if limit <= 0 || limit > 500 {
		limit = 64
	}
	campRows, err := s.DB.QueryContext(ctx,
		`SELECT campaign_id FROM fuzz_settle_outbox
		 WHERE status='pending'
		 GROUP BY campaign_id
		 ORDER BY MIN(id) ASC
		 LIMIT 32`)
	if err != nil {
		return nil, err
	}
	camps := make([]string, 0, 32)
	for campRows.Next() {
		var cid string
		if err := campRows.Scan(&cid); err != nil {
			campRows.Close()
			return nil, err
		}
		camps = append(camps, strings.TrimSpace(cid))
	}
	if err := campRows.Err(); err != nil {
		campRows.Close()
		return nil, err
	}
	campRows.Close()
	if len(camps) == 0 {
		return nil, nil
	}
	perCamp := limit
	if n := len(camps); n > 1 {
		perCamp = limit / min(n, 8)
		if perCamp < 8 {
			perCamp = 8
		}
	}
	if perCamp > limit {
		perCamp = limit
	}
	raw := make([]SettleOutboxItem, 0, limit)
	for _, cid := range camps {
		if cid == "" {
			continue
		}
		rows, qerr := s.DB.QueryContext(ctx,
			`SELECT id, campaign_id, kind, miner_address, severity, created_at, COALESCE(work_item_id,0)
			 FROM fuzz_settle_outbox
			 WHERE status='pending' AND campaign_id=?
			 ORDER BY CASE kind WHEN 'finalize' THEN 1 ELSE 0 END ASC, id ASC
			 LIMIT ?`, cid, perCamp)
		if qerr != nil {
			return nil, qerr
		}
		for rows.Next() {
			var it SettleOutboxItem
			if err := rows.Scan(&it.ID, &it.CampaignID, &it.Kind, &it.MinerAddress, &it.Severity, &it.CreatedAt, &it.WorkItemID); err != nil {
				rows.Close()
				return nil, err
			}
			raw = append(raw, it)
		}
		rerr := rows.Err()
		rows.Close()
		if rerr != nil {
			return nil, rerr
		}
		if len(raw) >= limit*2 {
			break
		}
	}
	return fairSettleOutboxWindow(raw, limit), nil
}

// fairSettleOutboxWindow round-robins pending rows across campaigns so settle-pull
// on node A can drain its escrows while an undrainable foreign head still exists.
func fairSettleOutboxWindow(items []SettleOutboxItem, limit int) []SettleOutboxItem {
	if limit <= 0 || len(items) == 0 {
		return nil
	}
	if len(items) <= limit {
		return items
	}
	type campQ struct {
		id    string
		items []SettleOutboxItem
	}
	order := make([]string, 0, 16)
	byCamp := map[string]*campQ{}
	for _, it := range items {
		cid := strings.TrimSpace(it.CampaignID)
		q := byCamp[cid]
		if q == nil {
			q = &campQ{id: cid}
			byCamp[cid] = q
			order = append(order, cid)
		}
		q.items = append(q.items, it)
	}
	if len(order) <= 1 {
		out := items
		if len(out) > limit {
			out = out[:limit]
		}
		return out
	}
	// Cap per campaign so at least ~4 campaigns fit in one window when possible.
	perCamp := limit / 2
	if n := len(order); n > 2 {
		perCamp = limit / min(n, 8)
	}
	if perCamp < 8 {
		perCamp = 8
	}
	if perCamp > limit {
		perCamp = limit
	}
	idx := make(map[string]int, len(order))
	out := make([]SettleOutboxItem, 0, limit)
	for len(out) < limit {
		progress := false
		for _, cid := range order {
			q := byCamp[cid]
			taken := idx[cid]
			if taken >= perCamp || taken >= len(q.items) {
				continue
			}
			out = append(out, q.items[taken])
			idx[cid] = taken + 1
			progress = true
			if len(out) >= limit {
				break
			}
		}
		if !progress {
			break
		}
	}
	// If caps left room, fill from global oldest remainder.
	if len(out) < limit {
		seen := make(map[int64]bool, len(out))
		for _, it := range out {
			seen[it.ID] = true
		}
		for _, it := range items {
			if seen[it.ID] {
				continue
			}
			out = append(out, it)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// AckSettleOutbox marks outbox rows applied by the origin node.
// Work-item settle_*_status is promoted to paid only when the outbox row was still
// pending — callers must Apply on-chain before ACK (stolen admin ACK without Apply
// still risks false paid; keep admin token offline / TLS-only for settle routes).
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
			 WHERE settle_run_outbox_id=? AND settle_run_status IN ('queued','pending','queued_floor','pending_floor')`, id)
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
