package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

// PUB-02: admin UPSERT must not keep prepaid_attested after raising budget beyond attest caps.
func TestMergeRegisterConfigDropsPrepaidOnBudgetRaise(t *testing.T) {
	existing := map[string]any{
		"orders_publish": true, "prepaid_attested": true,
		"prepaid_attest_budget_runs": 8, "prepaid_attest_budget_hmc": 1.0,
		"budget_hmc": 1.0,
	}
	incoming := map[string]any{
		"orders_publish": true, "budget_hmc": 1.0, "dig_package": "scan",
	}
	merged := mergeRegisterConfig(incoming, existing, 8, 8)
	if !configTruthyAny(merged, "prepaid_attested") {
		t.Fatal("same budget must keep prepaid_attested")
	}
	raised := mergeRegisterConfig(incoming, existing, 64, 8)
	if configTruthyAny(raised, "prepaid_attested") {
		t.Fatal("raised budget_runs must clear prepaid_attested")
	}
	hmcRaise := mergeRegisterConfig(map[string]any{
		"orders_publish": true, "budget_hmc": 50.0,
	}, existing, 8, 8)
	if configTruthyAny(hmcRaise, "prepaid_attested") {
		t.Fatal("raised budget_hmc must clear prepaid_attested")
	}
}

func TestRegisterCampaignClearsPrepaidWhenAdminRaisesRuns(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "pf.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := map[string]any{
		"pool_distributed": true, "orders_publish": true, "prepaid_attested": true,
		"budget_hmc": 1.0, "dig_package": "scan", "guard_pack": "secrets",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "camp-attest-caps", CampaignType: "property", Status: "running",
		OwnerRef: "customer:a", BudgetRuns: 8, BudgetSeconds: 60, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.CampaignConfig(ctx, "camp-attest-caps")
	if err != nil {
		t.Fatal(err)
	}
	if !configTruthyAny(got, "prepaid_attested") {
		t.Fatal("expected prepaid_attested after first register")
	}
	if configIntLoose(got, "prepaid_attest_budget_runs") != 8 {
		t.Fatalf("attest runs stamp=%v", got["prepaid_attest_budget_runs"])
	}
	// Admin-style UPSERT with higher budget_runs, no fresh prepaid stamp.
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "camp-attest-caps", CampaignType: "property", Status: "running",
		OwnerRef: "customer:a", BudgetRuns: 128, BudgetSeconds: 60,
		Config: map[string]any{
			"pool_distributed": true, "orders_publish": true,
			"budget_hmc": 1.0, "dig_package": "scan", "guard_pack": "secrets",
		},
	}); err != nil {
		t.Fatal(err)
	}
	got2, err := svc.CampaignConfig(ctx, "camp-attest-caps")
	if err != nil {
		t.Fatal(err)
	}
	if configTruthyAny(got2, "prepaid_attested") {
		t.Fatal("prepaid_attested must clear when budget_runs exceeds attest cap")
	}
	if err := svc.EnsureWorkItems(ctx, "camp-attest-caps", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=?`, "camp-attest-caps").Scan(&n); err != nil {
		t.Fatal(err)
	}
	// First register may have seeded 8; raise must not top up to 128 without prepaid.
	if n > 8 {
		t.Fatalf("unpaid raise must not enqueue beyond attested runs, got %d", n)
	}
}
