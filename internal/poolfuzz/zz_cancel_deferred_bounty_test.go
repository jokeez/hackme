package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestRecomputeProgressPreservesCancelled(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	svc := &Service{DB: db}
	id := "camp-cancel-resurrect"
	cfg := map[string]any{"pool_distributed": true, "work_kind": "hunt_shard", "budget_hmc": 10.0}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, BudgetSeconds: 1, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET started_at=? WHERE id=?`, now-10, id)
	if err := svc.EnsureWorkItems(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', result_ok=1, duration_ms=10, updated_at=? WHERE campaign_id=?`, now, id)
	if err := svc.SetCampaignStatus(ctx, id, "cancelled"); err != nil {
		t.Fatal(err)
	}
	completed, err := svc.recomputeProgress(ctx, id, now)
	if err != nil {
		t.Fatal(err)
	}
	if completed {
		t.Fatal("cancelled campaign must not report completed")
	}
	var st string
	if err := db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, id).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != "cancelled" {
		t.Fatalf("status=%q want cancelled (late retry must not resurrect)", st)
	}
}

func TestFlushDeferredBountiesSkipsCancelled(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "deferred.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	settler := &spySettler{}
	svc := &Service{DB: db, Settler: settler}
	id := "camp-deferred-cancel"
	cfg := map[string]any{
		"pool_distributed": true, "work_kind": "hunt_shard", "budget_hmc": 10.0,
		"bounty_requires_native": false,
	}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 8, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	_, err = db.ExecContext(ctx,
		`INSERT INTO fuzz_findings (id, campaign_id, finding_type, severity, title, input_sha256, detail_json, created_at)
		 VALUES ('f1', ?, 'crash', 'high', 't', 'aa', '{"miner_address":"HMC-miner0000000001","item_id":1}', ?)`,
		id, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO fuzz_native_queue (finding_id, campaign_id, status, created_at, updated_at)
		 VALUES ('f1', ?, 'confirmed', ?, ?)`, id, now, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SetCampaignStatus(ctx, id, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if err := svc.flushDeferredBounties(ctx); err != nil {
		t.Fatal(err)
	}
	settler.mu.Lock()
	paid := settler.findings
	settler.mu.Unlock()
	if paid != 0 {
		t.Fatalf("cancelled campaign paid deferred bounty %d times", paid)
	}
}
