package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestTryFinalizeEscrowSkipsWhenOutboxPending(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "fin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	id := "fin-gate-pending"
	cfg := map[string]any{"pool_distributed": true, "budget_hmc": 10, "escrow_split": "50_50"}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.EnqueueSettleOutbox(ctx, "run", id, "HMC-1234567890123456", "", 1); err != nil {
		t.Fatal(err)
	}
	if err := svc.tryFinalizeEscrow(ctx, id); err != nil {
		t.Fatal(err)
	}
	if spy.finalize != 0 {
		t.Fatalf("finalize calls=%d want 0 while outbox pending", spy.finalize)
	}
}

func TestTryFinalizeEscrowSkipsWhenWorkSettlePending(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "fin2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	id := "fin-gate-work"
	cfg := map[string]any{"pool_distributed": true, "budget_hmc": 10}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Status: "running", BudgetRuns: 2, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', miner_address=?, settle_run_status='pending_floor', updated_at=? WHERE campaign_id=? AND id=1`,
		"HMC-1234567890123456", now, id); err != nil {
		t.Fatal(err)
	}
	if err := svc.tryFinalizeEscrow(ctx, id); err != nil {
		t.Fatal(err)
	}
	if spy.finalize != 0 {
		t.Fatalf("finalize calls=%d want 0 while work settle pending", spy.finalize)
	}
}

func TestTryFinalizeEscrowRunsWhenClear(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "fin3.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	id := "fin-gate-ok"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2,
		Config: map[string]any{"budget_hmc": 10},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.tryFinalizeEscrow(ctx, id); err != nil {
		t.Fatal(err)
	}
	if spy.finalize != 1 {
		t.Fatalf("finalize calls=%d want 1", spy.finalize)
	}
}
