package poolfuzz

import (
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/hunt"
	"hackme/internal/store"
)

// Claims for hunt campaigns whose cfg carries seed_byte_corpus must ship the
// corpus so worker-side exec input derivation matches the verification replay.
func TestClaimShipsSeedByteCorpus(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "r21.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()

	corpus := []any{"deadbeef01020304", "cafebabefeedface", "4142434445464748"}
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "jsmn",
		"harness_hash":         "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"iterations_per_shard": 2,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
		"seed_byte_corpus":     corpus,
	}
	id := "r21-camp"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", []byte("r21-harness-blob"), "jsmn"); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w21", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if len(w.SeedByteCorpus) != len(corpus) {
		t.Fatalf("SeedByteCorpus not shipped: len=%d want %d", len(w.SeedByteCorpus), len(corpus))
	}
	for i := range corpus {
		if w.SeedByteCorpus[i] != corpus[i] {
			t.Fatalf("SeedByteCorpus[%d]=%v want %v", i, w.SeedByteCorpus[i], corpus[i])
		}
	}
}

// Guided claims must ship seed_byte_corpus after L2 merge so worker mutator
// bases match async replay (which also merges from the same on-disk cache).
func TestClaimShipsSeedByteCorpusAfterL2Merge(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "jsmn")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "seed-a.bin"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "seed-b.bin"), []byte(`{"b":2}`), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := store.Open(filepath.Join(dir, "r21-l2.db"))
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
		"upstream_target_id":   "jsmn",
		"harness_hash":         "aabbccddeeff0011aabbccddeeff0011aabbccddeeff0011aabbccddeeff0011",
		"iterations_per_shard": 4,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "jsmn")
	id := "r21-l2-camp"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, "aabbccddeeff0011aabbccddeeff0011aabbccddeeff0011aabbccddeeff0011", []byte("r21-l2-harness"), "jsmn"); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w21-l2", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if len(w.SeedByteCorpus) < 2 {
		t.Fatalf("expected L2 seed_byte_corpus on claim, got %#v", w.SeedByteCorpus)
	}
	if len(w.CorpusSeeds) < 2 {
		t.Fatalf("expected guided corpus snapshot, got %d seeds", len(w.CorpusSeeds))
	}
}

// A campaign that already has a coverage unit must not re-read L2 on claim.
// The pinned head stays first; an on-disk-only file is not merged in front of it.
func TestClaimSkipsL2MergeWhenCoverageSeedPresent(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "jsmn")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "only-l2.bin"), []byte("only-on-disk-l2"), 0o644); err != nil {
		t.Fatal(err)
	}
	fresh := "feature-from-miner-a"
	db, err := store.Open(filepath.Join(dir, "r21-skip.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	hash := "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff"
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "jsmn",
		"harness_hash":         hash,
		"iterations_per_shard": 4,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
		"seed_byte_corpus":     []any{fresh},
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "jsmn")
	id := "r21-skip-l2"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, hash, []byte("r21-skip-harness"), "jsmn"); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w21-skip", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if len(w.SeedByteCorpus) == 0 || w.SeedByteCorpus[0] != fresh {
		t.Fatalf("pinned seed not first: %#v", w.SeedByteCorpus)
	}
	l2Hex := hex.EncodeToString([]byte("only-on-disk-l2"))
	for _, item := range w.SeedByteCorpus {
		s, _ := item.(string)
		if s == "only-on-disk-l2" || s == l2Hex {
			t.Fatalf("L2 file was merged on the claim path: %#v", w.SeedByteCorpus)
		}
	}
}

// A live campaign that stored a 90s wall must keep it, and claim must not
// compile, fetch git, or showmap. Boot-only JSON still walks L2 once.
func TestClaimKeepsStoredLeaseAndDoesNotCompile(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_LEASE_SEC", "300")
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	marker := filepath.Join(dir, "tool-invoked")
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho \"$0\" >> \"" + marker + "\"\nexit 99\n"
	for _, name := range []string{"clang", "clang++", "git", "afl-showmap", "afl-cc", "llvm-symbolizer"} {
		path := filepath.Join(bin, name)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	db, err := store.Open(filepath.Join(dir, "lease90.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	hash := "11223344556677889900aabbccddeeff11223344556677889900aabbccddeeff"
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "jsmn",
		"harness_hash":         hash,
		"iterations_per_shard": 4,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
		"hunt_engine":          "libfuzzer",
		"hunt_package":         "hunt_heavy",
		"engine_lease_sec":     90,
		"seed_byte_corpus":     []any{"feature-already-on-campaign"},
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "jsmn")
	id := "lease-90"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, hash, []byte("lease-90-harness"), "jsmn"); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w-lease90", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	if w.EngineLeaseSec != 90 {
		t.Fatalf("stored wall restamped to %d", w.EngineLeaseSec)
	}
	if _, err := os.Stat(marker); err == nil {
		body, _ := os.ReadFile(marker)
		t.Fatalf("claim invoked a compiler or showmap:\n%s", body)
	}
}

func TestClaimStillWalksL2WhenCorpusIsOnlyBootStarters(t *testing.T) {
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "0")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "0")
	dir := t.TempDir()
	t.Setenv("HACKME_REPO_ROOT", dir)
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", "jsmn")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "real.bin"), []byte(`{"found":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(dir, "boots.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	hash := "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899"
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "jsmn",
		"harness_hash":         hash,
		"iterations_per_shard": 4,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
		// Wire form is hex. "null" as a raw string is valid base64 and would
		// not be the boot starter the ranker recognizes.
		"seed_byte_corpus": []any{"7b7d", "5b5d", "6e756c6c", "00", "80"},
	}
	hunt.ApplyPoolGuidedDefaults(cfg, "jsmn")
	id := "boot-only"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "hunt", Status: "running", BudgetRuns: 1, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, hash, []byte("boot-harness"), "jsmn"); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "w-boots", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim ok=%v err=%v", ok, err)
	}
	want := hex.EncodeToString([]byte(`{"found":1}`))
	found := false
	for _, item := range w.SeedByteCorpus {
		s, _ := item.(string)
		if s == want || s == `{"found":1}` {
			found = true
		}
	}
	if !found {
		t.Fatalf("boot-only corpus skipped the L2 walk: %#v", w.SeedByteCorpus)
	}
}
