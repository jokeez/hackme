package poolfuzz

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/hunt"
	"hackme/internal/store"
)

func TestSpreadCatalogClaimSeedsRotatesAndSkipsInventory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	seedDir := hunt.LibFuzzerSeedDir(dir, "mpack")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i := 0; i < 120; i++ {
		path := filepath.Join(seedDir, fmt.Sprintf("unit-%03d.bin", i))
		if err := os.WriteFile(path, []byte(fmt.Sprintf("unit-%03d", i)), 0o600); err != nil {
			t.Fatal(err)
		}
		mt := now.Add(time.Duration(i) * time.Second)
		if err := os.Chtimes(path, mt, mt); err != nil {
			t.Fatal(err)
		}
	}
	first := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "catalog",
		"upstream_target_id": "mpack",
	}
	second := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "catalog",
		"upstream_target_id": "mpack",
	}
	spreadCatalogClaimSeeds(first, 0)
	spreadCatalogClaimSeeds(second, 1)
	if !seedCorpusHas(first["seed_byte_corpus"].([]any), hex.EncodeToString([]byte("unit-119"))) {
		t.Fatal("newest unit missing")
	}
	if !seedCorpusHas(second["seed_byte_corpus"].([]any), hex.EncodeToString([]byte("unit-119"))) {
		t.Fatal("newest unit missing on the next shard")
	}
	if seedCorpusHas(first["seed_byte_corpus"].([]any), hex.EncodeToString([]byte("unit-017"))) {
		t.Fatal("first shard took the next shard's older slice")
	}
	if !seedCorpusHas(second["seed_byte_corpus"].([]any), hex.EncodeToString([]byte("unit-017"))) {
		t.Fatal("next shard missed its older slice")
	}
	inv := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "inventory",
		"upstream_target_id": "mpack",
		"seed_byte_corpus":   []any{"aa"},
	}
	spreadCatalogClaimSeeds(inv, 0)
	if seedCorpusHas(inv["seed_byte_corpus"].([]any), hex.EncodeToString([]byte("unit-119"))) {
		t.Fatal("inventory claim read the shared catalog cache")
	}
}

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

func TestCatalogClaimFollowsSharedCacheGeneration(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "mpack")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "early.bin"), []byte("from-earlier-order"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "gen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	hash := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	pinned := "70696e6e6564"
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"hunt_source":          "catalog",
		"upstream_target_id":   "mpack",
		"harness_hash":         hash,
		"iterations_per_shard": 4,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
		"seed_byte_corpus":     []any{pinned},
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "mpack")
	id := "mpack-gen"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, hash, []byte("gen-harness"), "mpack"); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, id, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	early := hex.EncodeToString([]byte("from-earlier-order"))
	w, ok, err := svc.Claim(ctx, "w-gen-1", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if len(w.SeedByteCorpus) == 0 || w.SeedByteCorpus[0] != pinned {
		t.Fatalf("own unit not first: %#v", w.SeedByteCorpus)
	}
	if !seedCorpusHas(w.SeedByteCorpus, early) {
		t.Fatalf("earlier shared unit missing: %#v", w.SeedByteCorpus)
	}

	// Unchanged cache, stamp already stored. A file that is not in the corpus
	// must stay out while the generation matches.
	gen, err := hunt.SharedSeedGeneration(dir, "mpack")
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	stored := parseConfigJSON(raw)
	if jsonString(stored["catalog_seed_gen"]) != gen {
		t.Fatalf("stamp %q want %q", jsonString(stored["catalog_seed_gen"]), gen)
	}
	outside := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(seedDir, "planted")); err != nil {
		t.Fatal(err)
	}
	if err := svc.mergeCatalogSeedCache(ctx, id, stored); err != nil {
		t.Fatal(err)
	}
	if seedCorpusHas(stored["seed_byte_corpus"].([]any), hex.EncodeToString([]byte("secret-bytes"))) {
		t.Fatal("symlink target entered the corpus")
	}

	if err := os.WriteFile(filepath.Join(seedDir, "later.bin"), []byte("from-later-order"), 0o644); err != nil {
		t.Fatal(err)
	}
	w2, ok, err := svc.Claim(ctx, "w-gen-2", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("second claim ok=%v err=%v", ok, err)
	}
	later := hex.EncodeToString([]byte("from-later-order"))
	if !seedCorpusHas(w2.SeedByteCorpus, later) {
		t.Fatalf("later shared unit missing: %#v", w2.SeedByteCorpus)
	}
	if len(w2.SeedByteCorpus) == 0 || w2.SeedByteCorpus[0] != pinned {
		t.Fatalf("own unit lost the front: %#v", w2.SeedByteCorpus)
	}
}

func TestFullCatalogCorpusStillAbsorbsSharedSeed(t *testing.T) {
	t.Setenv("HACKME_POOL_SEED_CORPUS_MAX_ITEMS", "4")
	t.Setenv("HACKME_POOL_SEED_CORPUS_MAX_BYTES", "100000")
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "mpack")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "other.bin"), []byte("from-other-order"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "full.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"hunt_source":          "catalog",
		"upstream_target_id":   "mpack",
		"seed_byte_corpus":     []any{"aa", "bb", "cc", "dd"},
		"iterations_per_shard": 2,
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "mpack")
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "mpack-full", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureGuidedCorpusSeeded(ctx, "mpack-full", cfg, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	list, _ := cfg["seed_byte_corpus"].([]any)
	if len(list) == 0 || list[0] != "aa" {
		t.Fatalf("own front lost: %#v", list)
	}
	if !seedCorpusHas(list, hex.EncodeToString([]byte("from-other-order"))) {
		t.Fatalf("shared unit dropped at cap: %#v", list)
	}
	if len(list) > 4 {
		t.Fatalf("corpus grew past the wire cap: %d", len(list))
	}

	if err := os.WriteFile(filepath.Join(seedDir, "later.bin"), []byte("later-unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureGuidedCorpusSeeded(ctx, "mpack-full", cfg, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	list, _ = cfg["seed_byte_corpus"].([]any)
	if !seedCorpusHas(list, hex.EncodeToString([]byte("later-unit"))) {
		t.Fatalf("later unit skipped after stamp: %#v", list)
	}
	if len(list) == 0 || list[0] != "aa" {
		t.Fatalf("own front lost on second fold: %#v", list)
	}
}

func TestPersistDoesNotConsumeUnmergedGeneration(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "mpack")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "early.bin"), []byte("early-unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "stamp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"hunt_source":          "catalog",
		"upstream_target_id":   "mpack",
		"seed_byte_corpus":     []any{"aa"},
		"iterations_per_shard": 2,
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "mpack")
	id := "mpack-stamp"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureGuidedCorpusSeeded(ctx, id, cfg, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "other.bin"), []byte("other-campaign"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := svc.persistAcceptedClaimSeeds(ctx, id, catalogSharedTarget(cfg), []fuzzengine.PoolCorpusSeed{
		{InputBytes: []byte("fresh-unit"), Energy: 2},
	}); err != nil {
		t.Fatal(err)
	}
	var raw string
	if err := db.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	stored := parseConfigJSON(raw)
	gen, err := hunt.SharedSeedGeneration(dir, "mpack")
	if err != nil {
		t.Fatal(err)
	}
	if jsonString(stored["catalog_seed_gen"]) == gen {
		t.Fatal("persist stamped a generation this campaign has not folded")
	}
	if err := svc.mergeCatalogSeedCache(ctx, id, stored); err != nil {
		t.Fatal(err)
	}
	list, _ := stored["seed_byte_corpus"].([]any)
	if !seedCorpusHas(list, hex.EncodeToString([]byte("other-campaign"))) {
		t.Fatalf("other campaign unit missing after fold: %#v", list)
	}
	if len(list) == 0 || list[0] != hex.EncodeToString([]byte("fresh-unit")) {
		t.Fatalf("just-accepted unit not first: %#v", list)
	}
}

func TestCatalogLeaseSeedWindowSkipsUnchangedAndInventory(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	db, err := store.Open(filepath.Join(dir, "lease.db"))
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
		"pool_distributed":   true,
		"work_kind":          "hunt_shard",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "lease-mpack", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, "lease-mpack", time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	var itemID int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=?`, "lease-mpack").Scan(&itemID); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=? WHERE id=?`, "w1", now+60, itemID); err != nil {
		t.Fatal(err)
	}
	seedDir := hunt.LibFuzzerSeedDir(dir, "mpack")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "unit.bin"), []byte("from-other-shard"), 0o600); err != nil {
		t.Fatal(err)
	}
	gen, seeds, unchanged, err := svc.CatalogLeaseSeedWindow(ctx, "w1", "lease-mpack", itemID, "", 0)
	if err != nil || unchanged || gen == "" || len(seeds) != 1 || string(seeds[0]) != "from-other-shard" {
		t.Fatalf("gen=%q unchanged=%v seeds=%q err=%v", gen, unchanged, seeds, err)
	}
	_, again, unchanged, err := svc.CatalogLeaseSeedWindow(ctx, "w1", "lease-mpack", itemID, gen, 0)
	if err != nil || !unchanged || len(again) != 0 {
		t.Fatalf("second pull unchanged=%v n=%d err=%v", unchanged, len(again), err)
	}
	if _, _, _, err := svc.CatalogLeaseSeedWindow(ctx, "other", "lease-mpack", itemID, "", 0); err == nil {
		t.Fatal("lease holder was not checked")
	}
	inv := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "inventory",
		"upstream_target_id": "mpack",
		"pool_distributed":   true,
		"work_kind":          "hunt_shard",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "lease-inv", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: inv}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, "lease-inv", now); err != nil {
		t.Fatal(err)
	}
	var invItem int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=?`, "lease-inv").Scan(&invItem); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=? WHERE id=?`, "w1", now+60, invItem); err != nil {
		t.Fatal(err)
	}
	_, seeds, unchanged, err = svc.CatalogLeaseSeedWindow(ctx, "w1", "lease-inv", invItem, "", 0)
	if err != nil || !unchanged || len(seeds) != 0 {
		t.Fatalf("inventory pull unchanged=%v n=%d err=%v", unchanged, len(seeds), err)
	}
}

func TestCatalogMidWallPublishReachesNextLease(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	db, err := store.Open(filepath.Join(dir, "midwall.db"))
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
		"pool_distributed":   true,
		"work_kind":          "hunt_shard",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "mid-mpack", CampaignType: "hunt", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	if err := svc.EnsureWorkItems(ctx, "mid-mpack", now); err != nil {
		t.Fatal(err)
	}
	var itemIDs []int64
	rows, err := db.QueryContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=? ORDER BY id`, "mid-mpack")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		itemIDs = append(itemIDs, id)
	}
	rows.Close()
	if len(itemIDs) < 2 {
		t.Fatalf("want two leases, got %v", itemIDs)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=? WHERE id=?`, "w1", now+120, itemIDs[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=? WHERE id=?`, "w2", now+120, itemIDs[1]); err != nil {
		t.Fatal(err)
	}

	corpus := filepath.Join(dir, "live-corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Now()
	plant := func(name string, body []byte) {
		t.Helper()
		path := filepath.Join(corpus, name)
		if err := os.WriteFile(path, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	plant("old.bin", []byte("already-on-disk"))
	plant("grown.bin", []byte("grown-mid-wall"))
	plant("boot.json", []byte("{}"))
	plant("arr.json", []byte("[]"))
	plant("null.json", []byte("null"))
	plant("z.bin", []byte{0x00})
	plant("hi.bin", []byte{0x80})
	plant("crash-deadbeef", []byte("crash-artifact"))
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("symlink-secret-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(corpus, "via-link.bin")); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("already-on-disk"))
	units := hunt.CollectLibFuzzerPublishUnits(corpus, map[string]struct{}{
		hex.EncodeToString(sum[:]): {},
	}, 0, 0)
	if len(units) != 1 || string(units[0]) != "grown-mid-wall" {
		t.Fatalf("collector = %q", units)
	}

	inv := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "inventory",
		"upstream_target_id": "mpack",
		"pool_distributed":   true,
		"work_kind":          "hunt_shard",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "mid-inv", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: inv}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, "mid-inv", now); err != nil {
		t.Fatal(err)
	}
	var invItem int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=?`, "mid-inv").Scan(&invItem); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=? WHERE id=?`, "w1", now+120, invItem); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.PublishCatalogLeaseSeeds(ctx, "w1", "mid-inv", invItem, []fuzzengine.PoolCorpusSeed{
		{InputBytes: []byte("inventory-secret-unit")},
	}); err != nil || n != 0 {
		t.Fatalf("inventory publish n=%d err=%v", n, err)
	}
	cust := map[string]any{
		"campaign_type":      "hunt",
		"hunt_source":        "customer_repo",
		"upstream_target_id": "mpack",
		"pool_distributed":   true,
		"work_kind":          "hunt_shard",
	}
	if err := svc.RegisterCampaign(ctx, Campaign{ID: "mid-cust", CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cust}); err != nil {
		t.Fatal(err)
	}
	if err := svc.EnsureWorkItems(ctx, "mid-cust", now); err != nil {
		t.Fatal(err)
	}
	var custItem int64
	if err := db.QueryRowContext(ctx, `SELECT id FROM fuzz_work_items WHERE campaign_id=?`, "mid-cust").Scan(&custItem); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fuzz_work_items SET status='leased', lease_owner=?, lease_until=? WHERE id=?`, "w1", now+120, custItem); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.PublishCatalogLeaseSeeds(ctx, "w1", "mid-cust", custItem, []fuzzengine.PoolCorpusSeed{
		{InputBytes: []byte("customer-secret-unit")},
	}); err != nil || n != 0 {
		t.Fatalf("customer publish n=%d err=%v", n, err)
	}
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "mpack")
	if _, err := os.Stat(seedDir); !os.IsNotExist(err) {
		t.Fatalf("non-catalog publish created the shared cache: %v", err)
	}
	if _, err := svc.PublishCatalogLeaseSeeds(ctx, "other", "mid-mpack", itemIDs[0], []fuzzengine.PoolCorpusSeed{
		{InputBytes: []byte("grown-mid-wall")},
	}); err == nil || !strings.Contains(err.Error(), "active lease") {
		t.Fatalf("missing lease err=%v", err)
	}

	posted := []fuzzengine.PoolCorpusSeed{
		{InputBytes: units[0]},
		{InputBytes: []byte("crash-artifact"), Crash: true},
		{InputBytes: []byte("{}")},
		{InputBytes: []byte("[]")},
		{InputBytes: []byte("null")},
		{InputBytes: []byte{0x00}},
		{InputBytes: []byte{0x80}},
	}
	n, err := svc.PublishCatalogLeaseSeeds(ctx, "w1", "mid-mpack", itemIDs[0], posted)
	if err != nil || n != 1 {
		t.Fatalf("catalog publish n=%d err=%v", n, err)
	}
	got := map[string]bool{}
	ents, err := os.ReadDir(seedDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range ents {
		b, err := os.ReadFile(filepath.Join(seedDir, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		got[string(b)] = true
	}
	if !got["grown-mid-wall"] {
		t.Fatalf("shared cache = %#v", got)
	}
	for _, banned := range []string{"already-on-disk", "crash-artifact", "{}", "[]", "null", "symlink-secret-bytes", "inventory-secret-unit", "customer-secret-unit", string([]byte{0x00}), string([]byte{0x80})} {
		if got[banned] {
			t.Fatalf("published %q: %#v", banned, got)
		}
	}
	_, seen, unchanged, err := svc.CatalogLeaseSeedWindow(ctx, "w2", "mid-mpack", itemIDs[1], "", 0)
	if err != nil || unchanged || len(seen) != 1 || string(seen[0]) != "grown-mid-wall" {
		t.Fatalf("second lease unchanged=%v seeds=%q err=%v", unchanged, seen, err)
	}
}

func seedCorpusHas(list []any, want string) bool {
	for _, item := range list {
		s, _ := item.(string)
		if s == want {
			return true
		}
	}
	return false
}
