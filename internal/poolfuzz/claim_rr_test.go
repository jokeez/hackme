package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestCampaignClaimTier(t *testing.T) {
	if got := campaignClaimTier("campaign-bootstrap-expat-x", "HackMe Bootstrap", ""); got != "bootstrap" {
		t.Fatalf("bootstrap id: %q", got)
	}
	if got := campaignClaimTier("campaign-audit-1", "cust", "acme-corp"); got != "customer" {
		t.Fatalf("customer: %q", got)
	}
	if got := campaignClaimTier("campaign-audit-2", "x", "qa:phasing"); got != "other" {
		t.Fatalf("qa: %q", got)
	}
}

func TestClaimRoundRobinPrefersCustomerOverBootstrapWall(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "claim-rr.db"))
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
	// Large bootstrap backlog (simulates deep pool wall).
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "campaign-bootstrap-wall", CampaignType: "property", Status: "running",
		Title: "HackMe Bootstrap Audit · wall", BudgetRuns: 200, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	// Fresh customer campaign — must get claims under RR (not buried in FIFO).
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "campaign-customer-fast", CampaignType: "property", Status: "running",
		Title: "Customer pilot", OwnerRef: "customer:pilot", BudgetRuns: 8, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	customerHits := 0
	for i := 0; i < 20; i++ {
		w, ok, err := svc.Claim(ctx, "worker-bench", now)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatalf("claim %d empty", i)
		}
		if w.CampaignID == "campaign-customer-fast" {
			customerHits++
		}
	}
	// Customer must be drained first (8 budget) before bootstrap wall is touched.
	if customerHits < 8 {
		t.Fatalf("customer hits=%d want >=8 (starved by bootstrap FIFO/RR?)", customerHits)
	}
}
