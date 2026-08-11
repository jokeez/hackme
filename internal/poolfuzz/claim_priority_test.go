package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestRegisterCampaignPersistsOwnerRef(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "owner-ref.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	cfg := map[string]any{
		"pool_distributed": true,
		"check_semantics":  "detector",
		"wasm_check_hex":   "0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "campaign-customer-owner", CampaignType: "property", Status: "running",
		Title: "Acme audit", OwnerRef: "customer:acme-corp", BudgetRuns: 8, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	var owner string
	if err := db.QueryRowContext(ctx,
		`SELECT COALESCE(owner_ref,'') FROM fuzz_campaigns WHERE id=?`, "campaign-customer-owner").Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if owner != "customer:acme-corp" {
		t.Fatalf("owner_ref=%q want customer:acme-corp", owner)
	}
	if got := campaignClaimTier("campaign-customer-owner", "Acme audit", owner); got != "customer" {
		t.Fatalf("tier=%q want customer", got)
	}
}

func TestClaimCustomerBeatsExpiredBootstrapLease(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "claim-priority.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	cfg := map[string]any{
		"pool_distributed": true,
		"check_semantics":  "detector",
		"wasm_check_hex":   "0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "campaign-bootstrap-lease", CampaignType: "property", Status: "running",
		Title: "HackMe Bootstrap · lease trap", BudgetRuns: 16, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "campaign-customer-priority", CampaignType: "property", Status: "running",
		Title: "Customer order", OwnerRef: "customer:priority", BudgetRuns: 8, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	// Seed bootstrap with an expired lease (would win old phase-1 reclaim path).
	if _, err := db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='leased', lease_owner='other-worker', lease_until=?, updated_at=?
		 WHERE campaign_id='campaign-bootstrap-lease' AND id=(
		   SELECT id FROM fuzz_work_items WHERE campaign_id='campaign-bootstrap-lease' LIMIT 1
		 )`, now-60, now); err != nil {
		t.Fatal(err)
	}

	customerHits := 0
	for i := 0; i < 8; i++ {
		w, ok, err := svc.Claim(ctx, "worker-bench", now)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("claim %d empty", i)
		}
		if w.CampaignID == "campaign-customer-priority" {
			customerHits++
		} else if w.CampaignID == "campaign-bootstrap-lease" {
			t.Fatalf("claim %d took expired bootstrap lease before customer drained", i)
		}
	}
	if customerHits < 8 {
		t.Fatalf("customer hits=%d want >=8 (expired bootstrap lease starved order?)", customerHits)
	}
}
