package poolfuzz

import (
	"context"
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
