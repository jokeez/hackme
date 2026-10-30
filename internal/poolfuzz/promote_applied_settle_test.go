package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestPromoteAppliedSettleIntentsHealsQueued(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "promote.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	svc := &Service{DB: db}
	camp := "camp-promote-1"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: camp, CampaignType: "hunt", Status: "running", BudgetRuns: 2,
		Config: map[string]any{"budget_hmc": 10, "pool_distributed": true},
	}); err != nil {
		t.Fatal(err)
	}
	outboxID, err := svc.EnqueueSettleOutbox(ctx, "run", camp, "HMC-1111111111111111", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_settle_outbox SET status='applied', applied_at=? WHERE id=?`, now, outboxID); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, camp, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', miner_address=?, settle_run_status='queued', settle_run_outbox_id=?, updated_at=?
		 WHERE campaign_id=? AND id=1`,
		"HMC-1111111111111111", outboxID, now, camp); err != nil {
		t.Fatal(err)
	}
	n, err := svc.PromoteAppliedSettleIntents(ctx, camp)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("promoted=%d want 1", n)
	}
	var st string
	if err := db.QueryRowContext(ctx,
		`SELECT settle_run_status FROM fuzz_work_items WHERE campaign_id=? AND id=1`, camp).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != "paid" {
		t.Fatalf("status=%q want paid", st)
	}
}

func TestTryFinalizeEscrowPromotesAppliedThenFinalizes(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "promote-fin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	spy := &spySettler{}
	svc := &Service{DB: db, Settler: spy}
	camp := "camp-promote-fin"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: camp, CampaignType: "hunt", Status: "running", BudgetRuns: 2,
		Config: map[string]any{"budget_hmc": 10},
	}); err != nil {
		t.Fatal(err)
	}
	outboxID, err := svc.EnqueueSettleOutbox(ctx, "run", camp, "HMC-1111111111111111", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_settle_outbox SET status='applied', applied_at=? WHERE id=?`, now, outboxID); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, camp, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', miner_address=?, settle_run_status='queued', settle_run_outbox_id=?, updated_at=?
		 WHERE campaign_id=? AND id=1`,
		"HMC-1111111111111111", outboxID, now, camp); err != nil {
		t.Fatal(err)
	}
	if err := svc.tryFinalizeEscrow(ctx, camp); err != nil {
		t.Fatal(err)
	}
	if spy.finalize != 1 {
		t.Fatalf("finalize=%d want 1 after promote heal", spy.finalize)
	}
}
