package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

// A customer Hunt campaign (phase 1) must not starve Hunt-incapable dig workers: with
// SkipHunt they get the dig campaign's work, Hunt-capable workers still get Hunt first,
// and a filtered miss never arms the global empty-claim cache.
func TestClaimSkipHuntReachesDigWorkBehindCustomerHunt(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "0")
	db, err := store.Open(filepath.Join(t.TempDir(), "caps.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()

	hcfg := map[string]any{
		"pool_distributed": true, "work_kind": "hunt_shard", "campaign_type": "hunt",
		"upstream_target_id": "jsmn", "harness_hash": "abcdef0123456789",
		"iterations_per_shard": 4, "max_input_bytes": 256, "depth_tier": "oss_cve",
	}
	hunt.ApplyPoolGuidedDefaults(hcfg, "jsmn")
	huntID := "hunt-customer-caps"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: huntID, CampaignType: "hunt", Status: "running", BudgetRuns: 8, Config: hcfg, OwnerRef: "customer:acme"}); err != nil {
		t.Fatal(err)
	}
	putTestHuntHarness(t, ctx, db, "abcdef0123456789")
	_ = svc.EnsureWorkItems(ctx, huntID, now)

	dcfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true, "dig_package": "audit", "exec_per_unit": 4,
		"check_semantics": "detector", "wasm_check_hex": sandbox.MinimalGateWasmHex,
		"seed_corpus": []any{uint64(1)},
	}, "property")
	digID := "dig-caps"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: digID, CampaignType: "property", Status: "running", BudgetRuns: 8, Config: dcfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, digID, now)

	// Legacy Claim (no caps): customer Hunt wins phase 1 — the starvation precondition.
	w, ok, err := svc.Claim(ctx, "probe-any", now)
	if err != nil || !ok || w.CampaignID != huntID {
		t.Fatalf("baseline claim: ok=%v err=%v campaign=%q, want %q", ok, err, w.CampaignID, huntID)
	}
	if _, err := svc.ReleaseWorkLease(ctx, w.CampaignID, w.ItemID, "probe-any"); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		w, ok, err = svc.ClaimWithOptions(ctx, "dig-only-1", now, ClaimOptions{SkipHunt: true})
		if err != nil || !ok {
			t.Fatalf("dig-only claim %d: ok=%v err=%v", i, ok, err)
		}
		if w.CampaignID != digID || w.TaskClass == "hunt" {
			t.Fatalf("dig-only claim %d got campaign %q class %q", i, w.CampaignID, w.TaskClass)
		}
	}

	w, ok, err = svc.ClaimWithOptions(ctx, "hunt-capable-1", now, ClaimOptions{})
	if err != nil || !ok || w.CampaignID != huntID {
		t.Fatalf("hunt-capable claim: ok=%v err=%v campaign=%q", ok, err, w.CampaignID)
	}
	if n, err := svc.PendingHuntItems(ctx); err != nil || n != 1 {
		t.Fatalf("PendingHuntItems = %d, %v; want 1", n, err)
	}
}

// When only Hunt work exists, a SkipHunt miss must leave Hunt claims for capable workers
// immediately possible (no negative cache armed by the filtered miss).
func TestClaimSkipHuntMissDoesNotArmEmptyCache(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	t.Setenv("HACKME_POOL_EMPTY_CLAIM_MS", "5000")
	db, err := store.Open(filepath.Join(t.TempDir(), "caps2.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()
	hcfg := map[string]any{
		"pool_distributed": true, "work_kind": "hunt_shard", "campaign_type": "hunt",
		"upstream_target_id": "jsmn", "harness_hash": "abcdef0123456789",
		"iterations_per_shard": 4, "max_input_bytes": 256, "depth_tier": "oss_cve",
	}
	hunt.ApplyPoolGuidedDefaults(hcfg, "jsmn")
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "hunt-only", CampaignType: "hunt", Status: "running", BudgetRuns: 4, Config: hcfg}); err != nil {
		t.Fatal(err)
	}
	putTestHuntHarness(t, ctx, db, "abcdef0123456789")
	_ = svc.EnsureWorkItems(ctx, "hunt-only", now)

	if _, ok, err := svc.ClaimWithOptions(ctx, "dig-only-2", now, ClaimOptions{SkipHunt: true}); err != nil || ok {
		t.Fatalf("dig-only on hunt-only pool: ok=%v err=%v, want empty", ok, err)
	}
	if _, ok, err := svc.Claim(ctx, "hunt-capable-2", now); err != nil || !ok {
		t.Fatalf("capable claim right after a filtered miss: ok=%v err=%v (negative cache armed?)", ok, err)
	}
}
