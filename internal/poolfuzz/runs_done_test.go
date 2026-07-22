package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestListPublicCampaignsHidesClosedEscrowZombie(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "zomb.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cid := "campaign-zombie-closed"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: cid, CampaignType: "property", Title: "audit still running label", Status: "running",
		BudgetRuns: 100, Config: map[string]any{"pool_distributed": true, "budget_hmc": 5.0},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO fuzz_campaign_escrow
		 (campaign_id, budget_units, runs_pool_units, bounty_pool_units, runs_paid_units, bounty_paid_units,
		  runs_done, budget_runs, per_run_units, finding_winner, status, created_at)
		 VALUES (?, 500000000, 100000000, 400000000, 0, 0, 0, 100, 1000000, '', 'closed', ?)`,
		cid, time.Now().Unix())
	if err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListPublicCampaigns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if toStringAny(it["id"]) == cid {
			t.Fatalf("closed-escrow zombie still listed: %+v", it)
		}
	}
}

func toStringAny(v any) string {
	s, _ := v.(string)
	return s
}

func TestRunsDoneForCampaignPrefersWorkItems(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cid := "campaign-pool-runs-done"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: cid, CampaignType: "property", Title: "audit", Status: "running",
		BudgetRuns: 8, Config: map[string]any{"pool_distributed": true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, cid, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 3; i++ {
		_, err = db.ExecContext(ctx,
			`UPDATE fuzz_work_items SET status='done', updated_at=? WHERE campaign_id=? AND input_n=?`,
			time.Now().Unix(), cid, i)
		if err != nil {
			t.Fatal(err)
		}
	}
	got := runsDoneForCampaign(ctx, db, cid, map[string]any{"runs_done": 0})
	if got != 3 {
		t.Fatalf("runs_done=%d want 3", got)
	}
	items, err := svc.ListPublicCampaigns(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || intFromJSON(items[0]["runs_done"]) != 3 {
		t.Fatalf("marketplace runs_done=%v want 3", items[0])
	}
}
