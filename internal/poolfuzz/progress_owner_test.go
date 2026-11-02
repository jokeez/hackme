package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestCampaignProgressOwnerSeesInternalGateID(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "owner-prog.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	// Ids containing "-probe-" are hidden from public marketplace progress (M9).
	cid := "hunt-customer-probe-heavy-1"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: cid, CampaignType: "hunt", Title: "customer heavy", Status: "running",
		BudgetRuns: 12000,
		Config: map[string]any{
			"pool_distributed": true,
			"orders_publish":   true,
			"hunt_package":     "hunt_heavy",
			"work_kind":        "hunt_shard",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CampaignProgress(ctx, cid); err == nil {
		t.Fatal("public progress must hide -probe- ids")
	}
	prog, err := svc.CampaignProgressOwner(ctx, cid)
	if err != nil {
		t.Fatalf("owner progress: %v", err)
	}
	if prog["id"] != cid {
		t.Fatalf("id=%v", prog["id"])
	}
	if intFromJSON(prog["budget_runs"]) != 12000 {
		t.Fatalf("budget_runs=%v", prog["budget_runs"])
	}
}
