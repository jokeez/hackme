package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

func TestShouldOmitFatCorpusSeeds(t *testing.T) {
	t.Setenv("HACKME_POOL_CLAIM_LIGHT", "1")
	t.Setenv("HACKME_POOL_CORPUS_DIR", "")
	SetCorpusObjectDir("")
	seeds := []fuzzengine.PoolCorpusSeed{{Input: 1}}
	if !ShouldOmitFatCorpusSeeds("abc", seeds) {
		t.Fatal("light=1 must omit fat seeds when sha present")
	}
	t.Setenv("HACKME_POOL_CLAIM_LIGHT", "0")
	if ShouldOmitFatCorpusSeeds("abc", seeds) {
		t.Fatal("light=0 must keep fat seeds")
	}
}

func TestCorpusSnapshotForLeaseRequiresOwner(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "light.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":  true,
		"guided_scheduling": true,
		"exec_per_unit":     4,
		"check_semantics":   "detector",
		"wasm_check_hex":    sandbox.MinimalGateWasmHex,
		"seed_corpus":       []any{uint64(1), uint64(2), uint64(3)},
	}, "property")
	id := "light-corpus"
	if err := svc.RegisterCampaign(ctx, Campaign{ID: id, CampaignType: "property", Status: "running", BudgetRuns: 2, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())
	w, ok, err := svc.Claim(ctx, "owner", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if len(w.CorpusSeeds) == 0 {
		t.Fatal("guided claim should freeze seeds in service layer")
	}
	seeds, sha, err := svc.CorpusSnapshotForLease(ctx, "owner", id, w.ItemID)
	if err != nil || len(seeds) == 0 || sha == "" {
		t.Fatalf("owner fetch: seeds=%d sha=%q err=%v", len(seeds), sha, err)
	}
	_, _, err = svc.CorpusSnapshotForLease(ctx, "thief", id, w.ItemID)
	if err == nil {
		t.Fatal("thief must not fetch corpus snapshot")
	}
}

func TestDigPoolExecTruthHubCap(t *testing.T) {
	t.Setenv("HACKME_POOL_EXEC_PER_UNIT_CAP", "256")
	cfg := map[string]any{
		"pool_distributed": true,
		"dig_package":      "deep",
		"exec_per_unit":    512,
	}
	tr := DigPoolExecTruth(cfg)
	if tr.Effective != 256 || !tr.CappedOnPool {
		t.Fatalf("truth=%+v", tr)
	}
	if tr.HonestyNote == "" {
		t.Fatal("expected honesty note when capped")
	}
}
