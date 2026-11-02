package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/store"
)

func TestFilterIDsWithPendingWorkSkipsEmptyCampaigns(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "filt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed": true,
		"check_semantics":  "detector",
		"wasm_check_hex":   "0061736d0100000001060160017e017f0302010007090105636865636b00000a0601040041010b",
	}, "property")

	withPending := "camp-has-pending"
	empty := "camp-no-pending"
	for _, id := range []string{withPending, empty} {
		if err := svc.RegisterCampaign(ctx, Campaign{
			ID: id, CampaignType: "property", Title: id, Status: "running",
			BudgetRuns: 2, Config: cfg,
		}); err != nil {
			t.Fatal(err)
		}
		_ = svc.EnsureWorkItems(ctx, id, now)
	}
	// Drain empty campaign to done so it has no pending rows.
	_, _ = db.ExecContext(ctx,
		`UPDATE fuzz_work_items SET status='done', result_ok=1, updated_at=? WHERE campaign_id=?`,
		now, empty)

	got := svc.filterIDsWithPendingWork(ctx, []string{empty, withPending, "missing-camp"})
	if len(got) != 1 || got[0] != withPending {
		t.Fatalf("filter got %#v want [%s]", got, withPending)
	}
}

func TestEmptyClaimNegativeCacheEnvOverride(t *testing.T) {
	t.Setenv("HACKME_POOL_EMPTY_CLAIM_MS", "900")
	d := emptyClaimNegativeCacheTTL()
	if d < 850*time.Millisecond || d > 950*time.Millisecond {
		t.Fatalf("env override TTL=%s", d)
	}
	t.Setenv("HACKME_POOL_EMPTY_CLAIM_MS", "")
	d = emptyClaimNegativeCacheTTL()
	if d < 350*time.Millisecond || d > 450*time.Millisecond {
		t.Fatalf("default TTL=%s", d)
	}
}
