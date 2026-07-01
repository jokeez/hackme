package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

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
