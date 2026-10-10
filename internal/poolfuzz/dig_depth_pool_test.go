package poolfuzz

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

// TestDigPoolDepthClaimSubmitSmoke: finalize → register → claim (dict/corpus/exec) → submit segment.
func TestDigPoolDepthClaimSubmitSmoke(t *testing.T) {
	t.Setenv("HACKME_POOL_EXEC_PER_UNIT_CAP", "")
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "")

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "dig-depth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	svc := &Service{DB: db}
	ctx := context.Background()

	cfg := fuzzingcli.FinalizeDigCampaignConfig(map[string]any{
		"pool_distributed": true,
		"dig_package":      "deep",
		"guard_pack":       "secrets",
		"input_mode":       "bytes",
		"depth_tier":       "bytes_corpus",
		"check_semantics":  "detector",
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
		"max_input_bytes":  256,
		"seed_byte_corpus": []any{"41414141", "42424242", "43434343"},
		// Pin below pool cap so claim/submit stay fast while still multi-exec.
		"exec_per_unit": 8,
	}, "deep", "secrets", dir)

	if !fuzzengine.GuidedSchedulingEnabled(cfg) {
		t.Fatal("expected guided after finalize")
	}
	if fuzzengine.ParseMutatorDict(cfg) == nil {
		t.Fatal("expected mutator dict after finalize")
	}

	id := "dig-depth-smoke"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Title: "dig depth", Status: "running",
		BudgetRuns: 1, BudgetSeconds: 120, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	_ = svc.EnsureWorkItems(ctx, id, time.Now().Unix())

	w, ok, err := svc.Claim(ctx, "dig-w1", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if w.ExecPerUnit != 8 {
		t.Fatalf("exec_per_unit=%d want 8 (pool cap default 64)", w.ExecPerUnit)
	}
	if len(w.MutatorDict) == 0 || string(w.MutatorDict[:4]) != "AKIA" {
		t.Fatalf("claim missing Dig mutator_dict: %q", w.MutatorDict)
	}
	if len(w.SeedByteCorpus) < 2 {
		t.Fatalf("claim missing seed_byte_corpus: %#v", w.SeedByteCorpus)
	}
	if len(w.CorpusSeeds) == 0 {
		t.Fatal("guided Dig claim must ship corpus snapshot")
	}
	if !w.CorpusExploreV2 {
		t.Fatal("deep Dig claim should mirror corpus_explore_v2")
	}

	err = svc.Submit(ctx, SubmitRequest{
		WorkerID: "dig-w1", WorkID: w.WorkID, CampaignID: w.CampaignID,
		ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
		CheckResult: 0, DurationMS: 5, SegmentExecDone: w.ExecPerUnit,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDigPoolClaimMutatorDictSurvivesConfigJSON(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "dig-dict.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()

	cfg := fuzzingcli.FinalizeDigCampaignConfig(map[string]any{
		"pool_distributed":  true,
		"dig_package":       "audit",
		"guard_pack":        "filter_utf8",
		"depth_tier":        "wasm_native",
		"guided_scheduling": true,
		"exec_per_unit":     4,
		"check_semantics":   "detector",
		"wasm_check_hex":    sandbox.MinimalGateWasmHex,
		"seed_corpus":       []any{uint64(1), uint64(2), uint64(3)},
	}, "audit", "filter_utf8", dir)

	// Persist + reload like Claim does.
	id := "dig-dict-json"
	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: id, CampaignType: "property", Status: "running",
		BudgetRuns: 1, BudgetSeconds: 60, Config: cfg,
	}); err != nil {
		t.Fatal(err)
	}
	var cfgJSON string
	if err := db.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, id).Scan(&cfgJSON); err != nil {
		t.Fatal(err)
	}
	reloaded := parseConfigJSON(cfgJSON)
	want := fuzzengine.ParseMutatorDict(cfg)
	got := fuzzengine.ParseMutatorDict(reloaded)
	if len(got) == 0 || string(got) != string(want) {
		t.Fatalf("config_json broke mutator_dict: got %q want %q", got, want)
	}

	w, ok, err := svc.Claim(ctx, "w-dict", time.Now().Unix())
	if err != nil || !ok {
		t.Fatalf("claim: ok=%v err=%v", ok, err)
	}
	if string(w.MutatorDict) != string(want) {
		t.Fatalf("claim dict=%q want %q", w.MutatorDict, want)
	}
}

func TestRegisterCampaignFinalizesExplicitDigPackage(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "dig-pkg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := &Service{DB: db}
	ctx := context.Background()

	if err := svc.RegisterCampaign(ctx, Campaign{
		ID: "dig-pkg", CampaignType: "property", Status: "running",
		BudgetRuns: 1, BudgetSeconds: 60,
		Config: map[string]any{
			"dig_package":     "deep",
			"guard_pack":      "secrets",
			"wasm_check_hex":  sandbox.MinimalGateWasmHex,
			"check_semantics": "detector",
		},
	}); err != nil {
		t.Fatal(err)
	}
	var cfgJSON string
	if err := db.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, "dig-pkg").Scan(&cfgJSON); err != nil {
		t.Fatal(err)
	}
	cfg := parseConfigJSON(cfgJSON)
	if !fuzzengine.GuidedSchedulingEnabled(cfg) {
		t.Fatal("RegisterCampaign with dig_package must finalize guided")
	}
	if len(fuzzengine.ParseMutatorDict(cfg)) == 0 {
		t.Fatal("RegisterCampaign with dig_package must finalize mutator_dict")
	}
}
