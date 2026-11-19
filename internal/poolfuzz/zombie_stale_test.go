package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

func TestCancelZeroProgressSkipsPrepaidCustomerHunt(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "co.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()
	paidCfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"prepaid_attested": true,
		"budget_hmc":       135.0,
		"hunt_package":     "hunt_heavy",
		"check_semantics":  "native_crash",
	}, "hunt")
	paid := "hunt-heavy-duktape-paid"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: paid, CampaignType: "hunt", Title: "Heavy ASAN · duktape", Status: "running",
		OwnerRef: "customer:bootstrap", BudgetRuns: 12000, Config: paidCfg,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET created_at=? WHERE id=?`, now-7200, paid)

	zombieCfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"check_semantics":  "detector",
		"wasm_check_hex":   "0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b",
	}, "property")
	zombie := "camp-gate-zombie"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: zombie, CampaignType: "property", Title: "gate zombie", Status: "running",
		BudgetRuns: 4, Config: zombieCfg,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET created_at=? WHERE id=?`, now-7200, zombie)

	n, err := svc.CancelZeroProgressPoolCampaigns(ctx, 3600, 20)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cancelled=%d want 1 (only unpaid gate)", n)
	}
	var paidSt, zombieSt string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, paid).Scan(&paidSt)
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, zombie).Scan(&zombieSt)
	if paidSt != "running" {
		t.Fatalf("prepaid status=%s want running", paidSt)
	}
	if zombieSt != "cancelled" {
		t.Fatalf("zombie status=%s want cancelled", zombieSt)
	}
}

func TestCancelZeroProgressUsesDBDoneCount(t *testing.T) {
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

	zombie := "camp-zero-progress"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: zombie, CampaignType: "property", Title: "zombie", Status: "running",
		BudgetRuns: 4, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	// Backdate created_at so minAge matches.
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET created_at=? WHERE id=?`, now-7200, zombie)
	// Stale summary claims progress but DB has zero done.
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET summary_json=? WHERE id=?`, `{"runs_done":0}`, zombie)

	n, err := svc.CancelZeroProgressPoolCampaigns(ctx, 3600, 20)
	if err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected cancel, got %d", n)
	}
	var st string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, zombie).Scan(&st)
	if st != "cancelled" {
		t.Fatalf("status=%s", st)
	}
}

func TestCancelStuckExpiredLeaseCampaigns(t *testing.T) {
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
	id := "camp-expired-leases"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "stuck leases", Status: "running",
		BudgetRuns: 2, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET created_at=? WHERE id=?`, now-7200, id)
	_, _ = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner='gone', lease_until=?, updated_at=? WHERE campaign_id=?`,
		now-600, now, id)

	n, err := svc.CancelStuckExpiredLeaseCampaigns(ctx, 3600, 20)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cancelled=%d want 1", n)
	}
	var st string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, id).Scan(&st)
	if st != "cancelled" {
		t.Fatalf("status=%s", st)
	}
	var leased int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='leased'`, id).Scan(&leased)
	if leased != 0 {
		t.Fatalf("leased left=%d", leased)
	}
}

func TestReclaimExpiredLeases(t *testing.T) {
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
	id := "camp-reclaim"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "reclaim", Status: "running",
		BudgetRuns: 1, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner='w', lease_until=?, updated_at=? WHERE campaign_id=?`,
		now-10, now, id)
	n, err := svc.ReclaimExpiredLeases(ctx, now)
	if err != nil || n != 1 {
		t.Fatalf("reclaim n=%d err=%v", n, err)
	}
	var st string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_work_items WHERE campaign_id=?`, id).Scan(&st)
	if st != "pending" {
		t.Fatalf("status=%s", st)
	}
}

func TestReconcileCompletesBudgetDoneCampaign(t *testing.T) {
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
	id := "camp-budget-done"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "done budget", Status: "running",
		BudgetRuns: 2, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', result_ok=1, updated_at=? WHERE campaign_id=?`, now, id)
	if err := svc.reconcileActiveCampaignWork(ctx, id, now); err != nil {
		t.Fatal(err)
	}
	var st string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, id).Scan(&st)
	if st != "completed" {
		t.Fatalf("status=%s want completed", st)
	}
}

func TestCancelHuntCampaignsMissingHarness(t *testing.T) {
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
		"campaign_type":    "hunt",
		"work_kind":        "hunt_shard",
		"harness_hash":     "abcdef0123456789",
		"check_semantics":  "native_crash",
	}, "hunt")
	id := "hunt-missing-harness"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "hunt", Title: "yyjson-class", Status: "running",
		BudgetRuns: 4, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(ctx, `UPDATE fuzz_campaigns SET created_at=? WHERE id=?`, now-1800, id)
	n, err := svc.CancelHuntCampaignsMissingHarness(ctx, 900, 20)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cancelled=%d want 1", n)
	}
	var st string
	_ = db.QueryRowContext(ctx, `SELECT status FROM fuzz_campaigns WHERE id=?`, id).Scan(&st)
	if st != "cancelled" {
		t.Fatalf("status=%s", st)
	}
}
