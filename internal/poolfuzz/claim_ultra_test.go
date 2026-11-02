package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestReleaseWorkLeaseReturnsPending(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "release-lease.db"))
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
		ID: "campaign-release-lease", CampaignType: "property", Status: "running",
		Title: "Release lease", BudgetRuns: 4, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	work, ok, err := svc.Claim(ctx, "worker-a", now)
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if _, err := svc.ReleaseWorkLease(ctx, work.CampaignID, work.ItemID, "worker-a"); err != nil {
		t.Fatal(err)
	}
	var status, owner string
	if err := db.QueryRowContext(ctx,
		`SELECT status, lease_owner FROM fuzz_work_items WHERE id=?`, work.ItemID).Scan(&status, &owner); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || owner != "" {
		t.Fatalf("after release status=%q owner=%q", status, owner)
	}
	// Same shard claimable again.
	work2, ok, err := svc.Claim(ctx, "worker-b", now)
	if err != nil || !ok {
		t.Fatalf("reclaim: ok=%v err=%v", ok, err)
	}
	if work2.ItemID != work.ItemID {
		t.Fatalf("expected same item %d got %d", work.ItemID, work2.ItemID)
	}
}

func TestEmptyClaimNegativeCache(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "empty-claim.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	now := time.Now().Unix()
	_, ok, err := svc.Claim(ctx, "worker-empty", now)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected no work")
	}
	svc.emptyClaimMu.Lock()
	until := svc.emptyClaimUntil
	svc.emptyClaimMu.Unlock()
	if until.IsZero() || !time.Now().Before(until) {
		t.Fatalf("empty claim cache not armed: until=%v", until)
	}
	remain := time.Until(until)
	// Default ~400ms negative cache (HACKME_POOL_EMPTY_CLAIM_MS override).
	if remain < 200*time.Millisecond || remain > 600*time.Millisecond {
		t.Fatalf("empty claim TTL out of range (want ~400ms): remain=%s", remain)
	}
	start := time.Now()
	_, ok, err = svc.Claim(ctx, "worker-empty", now)
	if err != nil || ok {
		t.Fatalf("cached claim: ok=%v err=%v", ok, err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("cached empty claim too slow: %s", time.Since(start))
	}
}
