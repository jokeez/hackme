package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

func TestRegisterAfterCancelRevivesWorkQueue(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "co.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()

	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"check_semantics":  "detector",
		"wasm_check_hex":   "0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b",
	}, "property")
	id := "pool-zombie-resync"

	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "zombie test", Status: "running",
		BudgetRuns: 8, BudgetSeconds: 3600, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetCampaignStatus(ctx, id, "cancelled"); err != nil {
		t.Fatal(err)
	}
	var pending int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='pending'`, id).Scan(&pending)
	if pending != 0 {
		t.Fatalf("after cancel want 0 pending, got %d", pending)
	}
	var completedAt int64
	_ = db.QueryRowContext(ctx, `SELECT completed_at FROM fuzz_campaigns WHERE id=?`, id).Scan(&completedAt)
	if completedAt == 0 {
		t.Fatal("cancelled campaign should have completed_at")
	}

	// Pool resync re-registers as running (bootstrap_resync_pool path).
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "zombie test", Status: "running",
		BudgetRuns: 8, BudgetSeconds: 3600, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	_ = db.QueryRowContext(ctx, `SELECT completed_at FROM fuzz_campaigns WHERE id=?`, id).Scan(&completedAt)
	if completedAt != 0 {
		t.Fatalf("re-register running should clear completed_at, got %d", completedAt)
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='pending'`, id).Scan(&pending)
	if pending == 0 {
		t.Fatal("re-register should revive pending work without manual SQL")
	}

	w, ok, err := svc.Claim(ctx, "worker-zombie-test", now)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || w.CampaignID != id {
		t.Fatalf("claim after resync: ok=%v work=%+v", ok, w)
	}
}

func TestRepairZombiePoolCampaigns(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "co.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := map[string]any{"pool_distributed": true, "check_semantics": "detector"}
	id := "pool-zombie-tick"

	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "tick repair", Status: "running",
		BudgetRuns: 4, BudgetSeconds: 3600, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.SetCampaignStatus(ctx, id, "cancelled"); err != nil {
		t.Fatal(err)
	}
	// Simulate resync bug: status forced running without RegisterCampaign upsert path.
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_campaigns SET status='running' WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}

	n, err := svc.RepairZombiePoolCampaigns(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("RepairZombiePoolCampaigns want 1, got %d", n)
	}
	var pending int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='pending'`, id).Scan(&pending)
	if pending == 0 {
		t.Fatal("repair should restore pending queue")
	}
}
