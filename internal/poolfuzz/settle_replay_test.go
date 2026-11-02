package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestReplayCampaignSettlesRespectsRunFloor(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "replay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	svc := &Service{DB: db}
	id := "camp-replay-floor"
	cfg := map[string]any{"pool_distributed": true, "work_kind": "hunt_shard", "escrow_split": "50_50"}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Status: "completed", BudgetRuns: 4, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	miner := "HMC-aaaaaaaaaaaaaaaa"
	now := int64(1_700_000_000)
	for _, row := range []struct {
		itemID int64
		st     string
	}{
		{1, "pending_floor"},
		{2, "queued_floor"},
		{3, "pending"},
		{4, "paid"},
		{5, ""}, // unstamped — must not invent PayRun
	} {
		if _, err := db.ExecContext(ctx, `
			INSERT INTO fuzz_work_items
			 (campaign_id, id, input_n, status, attempts, result_ok, miner_address, settle_run_status, created_at, updated_at)
			 VALUES (?, ?, ?, 'done', 1, 1, ?, ?, ?, ?)`,
			id, row.itemID, row.itemID, miner, row.st, now, now); err != nil {
			t.Fatal(err)
		}
	}
	runs, _, _, err := svc.ReplayCampaignSettles(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if runs != 3 {
		t.Fatalf("runs enqueued=%d want 3 (skip paid + empty)", runs)
	}
	rows, err := db.QueryContext(ctx,
		`SELECT kind, work_item_id FROM fuzz_settle_outbox WHERE campaign_id=? ORDER BY work_item_id`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[int64]string{}
	for rows.Next() {
		var kind string
		var wid int64
		if err := rows.Scan(&kind, &wid); err != nil {
			t.Fatal(err)
		}
		got[wid] = kind
	}
	if got[1] != "run_floor" || got[2] != "run_floor" {
		t.Fatalf("floor items: %v", got)
	}
	if got[3] != "run" {
		t.Fatalf("full run item: %v", got)
	}
	if _, ok := got[4]; ok {
		t.Fatalf("paid item must not re-enqueue: %v", got)
	}
}

func TestPromoteAppliedSettleIntentsMarksPaid(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "promote.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	svc := &Service{DB: db}
	id := "camp-promote"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Status: "completed", BudgetRuns: 1,
		Config: map[string]any{"pool_distributed": true},
	}); err != nil {
		t.Fatal(err)
	}
	now := int64(1_700_000_100)
	outID, err := svc.EnqueueSettleOutbox(ctx, "run_floor", id, "HMC-bbbbbbbbbbbbbbbb", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO fuzz_work_items
		 (campaign_id, id, input_n, status, attempts, result_ok, miner_address, settle_run_status, settle_run_outbox_id, created_at, updated_at)
		 VALUES (?, 1, 1, 'done', 1, 1, 'HMC-bbbbbbbbbbbbbbbb', 'queued_floor', ?, ?, ?)`,
		id, outID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_settle_outbox SET status='applied', applied_at=? WHERE id=?`, now, outID); err != nil {
		t.Fatal(err)
	}
	n, err := svc.PromoteAppliedSettleIntents(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("promote rows=%d", n)
	}
	var st string
	if err := db.QueryRowContext(ctx,
		`SELECT settle_run_status FROM fuzz_work_items WHERE campaign_id=? AND id=1`, id).Scan(&st); err != nil {
		t.Fatal(err)
	}
	if st != "paid" {
		t.Fatalf("settle_run_status=%q want paid", st)
	}
}
