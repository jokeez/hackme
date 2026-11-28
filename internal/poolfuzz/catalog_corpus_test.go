package poolfuzz

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
	"hackme/internal/store"
)

func TestCatalogSeedReachesTheNextCampaign(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	db, err := store.Open(filepath.Join(dir, "share.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	now := time.Now().Unix()

	first := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "catalog",
		"upstream_target_id": "mpack",
		"pool_distributed":   true,
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "mpack-a", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: first}); err != nil {
		t.Fatal(err)
	}
	unit := []byte(`{"from":"first"}`)
	if err := svc.persistAcceptedClaimSeeds(ctx, "mpack-a", catalogSharedTarget(first), []fuzzengine.PoolCorpusSeed{
		{InputBytes: unit, Energy: 2},
	}); err != nil {
		t.Fatal(err)
	}

	second := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "catalog",
		"upstream_target_id": "mpack",
		"pool_distributed":   true,
		"seed_byte_corpus":   []any{"7b7d"},
	}
	hunt.ApplyPoolGuidedDefaults(second, "mpack")
	if err := svc.EnsureGuidedCorpusSeeded(ctx, "mpack-b", second, now); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range fuzzengine.ParseByteCorpus(second) {
		if string(b) == string(unit) {
			found = true
		}
	}
	if !found {
		t.Fatalf("second catalog campaign did not see the shared unit: %#v", second["seed_byte_corpus"])
	}
}

func TestInventoryOrderDoesNotPublishOrReadSharedSeeds(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	db, err := store.Open(filepath.Join(dir, "priv.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()

	secret := []byte("customer-private-seed")
	inv := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "inventory",
		"upstream_target_id": "mpack",
		"pool_distributed":   true,
	}
	if catalogSharedTarget(inv) != "" {
		t.Fatal("inventory order must not name a shared catalog target")
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "inv-a", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: inv}); err != nil {
		t.Fatal(err)
	}
	if err := svc.persistAcceptedClaimSeeds(ctx, "inv-a", catalogSharedTarget(inv), []fuzzengine.PoolCorpusSeed{
		{InputBytes: secret, Energy: 2},
		{InputBytes: []byte("crash-bytes"), Energy: 8, Crash: true},
	}); err != nil {
		t.Fatal(err)
	}
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "mpack")
	if _, err := os.Stat(seedDir); !os.IsNotExist(err) {
		t.Fatalf("inventory order wrote the shared cache: %v", err)
	}

	// A catalog order must not pick up a file planted beside the cache under a
	// target id the sanitizer rejects.
	evil := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "catalog",
		"upstream_target_id": "../mpack",
	}
	if catalogSharedTarget(evil) != "" {
		t.Fatal("path-like target id must not share")
	}
}

func TestCrashUnitIsNotShared(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	db, err := store.Open(filepath.Join(dir, "crash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "catalog",
		"upstream_target_id": "mpack",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "mpack-crash", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := svc.persistAcceptedClaimSeeds(ctx, "mpack-crash", catalogSharedTarget(cfg), []fuzzengine.PoolCorpusSeed{
		{InputBytes: []byte("boom"), Energy: 8, Crash: true},
	}); err != nil {
		t.Fatal(err)
	}
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "mpack")
	if _, err := os.Stat(seedDir); !os.IsNotExist(err) {
		t.Fatalf("crash unit entered the shared cache: %v", err)
	}
}

func TestInventoryDeltaStaysOutOfSharedNamespace(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	db, err := store.Open(filepath.Join(dir, "inv-ns.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	prevResolve := resolveCoverageHarness
	prevExec := huntEngineSeedExec
	t.Cleanup(func() {
		resolveCoverageHarness = prevResolve
		huntEngineSeedExec = prevExec
	})
	resolveCoverageHarness = func(context.Context, string, string, ...hunt.CoverageHarnessOpts) (string, bool, error) {
		return "", false, nil
	}
	huntEngineSeedExec = func(context.Context, *Service, string, map[string]any, []byte) (bool, bool, string, error) {
		return false, false, "", nil
	}
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"hunt_source":          "inventory",
		"upstream_target_id":   "mpack",
		"harness_hash":         "abcdef0123456789",
		"iterations_per_shard": 2,
	}
	id := "inv-ns-" + filepath.Base(dir)
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	var itemID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=? LIMIT 1`, id).Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	leaseWorkItemForTest(t, ctx, db, id, itemID, "w1")
	secret := []byte("customer-only-bytes")
	out, err := svc.AcceptResearchCorpusDelta(ctx, ResearchCorpusDeltaRequest{
		WorkerID: "w1", CampaignID: id, ItemID: itemID, Namespace: "research:mpack",
		Seeds: []fuzzengine.PoolCorpusSeed{{InputBytes: secret, Energy: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.SeedsAccepted != 1 {
		t.Fatalf("seeds=%d", out.SeedsAccepted)
	}
	shared, err := svc.ListNamespaceCorpus(ctx, "research:mpack", 64)
	if err != nil {
		t.Fatal(err)
	}
	if len(shared) != 0 {
		t.Fatalf("inventory bytes landed in research:mpack (%d)", len(shared))
	}
	local, err := svc.loadPoolCorpusSeeds(ctx, id, 64)
	if err != nil || len(local) < 1 {
		t.Fatalf("campaign-local seeds=%d err=%v", len(local), err)
	}
}
