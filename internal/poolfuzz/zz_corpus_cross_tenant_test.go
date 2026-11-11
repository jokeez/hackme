package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/store"
)

// High (fimasw): Dig guard_pack must not share corpus across tenants.
func TestPackNamespaceCrossTenantDoesNotLeak(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "r36.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{DB: db}
	ctx := context.Background()

	pack, err := fuzzingcli.GuardPackFor("secrets")
	if err != nil {
		t.Skip("secrets pack unavailable:", err)
	}
	tenantCfg := func(owner string) map[string]any {
		cfg := map[string]any{
			"pool_distributed": true,
			"exec_per_unit":    1,
			"check_semantics":  "detector",
			"owner_ref":        owner,
			"corpus_persist":   true,
		}
		return fuzzengine.ApplyDepthTier(fuzzingcli.ApplyPackConfig(cfg, pack), fuzzengine.DepthWasmOnly)
	}
	cfgA, cfgB := tenantCfg("customer:alice"), tenantCfg("customer:bob")
	nsA := fuzzengine.CorpusPersistNamespace(cfgA)
	nsB := fuzzengine.CorpusPersistNamespace(cfgB)
	if nsA == "" || nsB == "" || nsA == nsB {
		t.Fatalf("want distinct tenant namespaces, got A=%q B=%q", nsA, nsB)
	}

	secretInput := []byte("GITHUB_PAT=ghp_te_leak_test_7890")
	if err := svc.exportNamespaceCorpus(ctx, cfgA, fuzzengine.PackInputBytesToU64(secretInput), secretInput, 9, 7, 3, false, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}

	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "tenantB", CampaignType: "property", Status: "running",
		BudgetRuns: 1, OwnerRef: "customer:bob", Config: cfgB,
	}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, "tenantB", time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "wB", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	for _, s := range w.CorpusSeeds {
		if string(s.InputBytes) == string(secretInput) {
			t.Fatalf("tenant B claim carries tenant A input %q from ns %q", s.InputBytes, nsA)
		}
	}
}
