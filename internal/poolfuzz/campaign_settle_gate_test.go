package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestCampaignAcceptsSettleAllowsDrainExceptCancelled(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "gate.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	svc := &Service{DB: db}
	id := "camp-settle-gate"
	cfg := map[string]any{"pool_distributed": true, "work_kind": "hunt_shard"}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 8, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if !svc.campaignAcceptsSettle(ctx, id) {
		t.Fatal("running must accept settle")
	}
	for _, st := range []string{"paused", "completed"} {
		if err := svc.SetCampaignStatus(ctx, id, st); err != nil {
			t.Fatalf("status %s: %v", st, err)
		}
		if !svc.campaignAcceptsSettle(ctx, id) {
			t.Fatalf("%s must still accept settle (drain in-flight floors)", st)
		}
		if err := svc.SetCampaignStatus(ctx, id, "running"); err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.SetCampaignStatus(ctx, id, "cancelled"); err != nil {
		t.Fatal(err)
	}
	if svc.campaignAcceptsSettle(ctx, id) {
		t.Fatal("cancelled must NOT accept settle")
	}
}
