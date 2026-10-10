package fuzzingcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/fuzzengine"
)

func TestApplyDigPowerSchedulingDeep(t *testing.T) {
	cfg := map[string]any{"depth_tier": "bytes_corpus", "power_mut_cap": 6}
	ApplyDigPowerScheduling(cfg, "deep")
	if fuzzengine.PowerMutCap(cfg) < 14 {
		t.Fatalf("cap=%v", cfg["power_mut_cap"])
	}
	if fuzzengine.MutationRounds(cfg) < 12 {
		t.Fatalf("rounds=%v", cfg["mutation_rounds"])
	}
	if cfg["guided_scheduling"] != true {
		t.Fatalf("guided=%v", cfg["guided_scheduling"])
	}
	if cfg["corpus_explore_v2"] != true {
		t.Fatalf("explore=%v", cfg["corpus_explore_v2"])
	}
	if _, ok := cfg["exec_per_unit"]; !ok || fuzzengine.ExecPerUnit(cfg) < 64 {
		t.Fatalf("exec_per_unit=%v", cfg["exec_per_unit"])
	}
}

func TestFinalizeDigCampaignConfigDeepPoolDepth(t *testing.T) {
	cfg := FinalizeDigCampaignConfig(map[string]any{
		"dig_package": "deep",
		"guard_pack":  "secrets",
	}, "deep", "secrets", t.TempDir())
	if !fuzzengine.GuidedSchedulingEnabled(cfg) {
		t.Fatal("deep Dig must enable guided_scheduling")
	}
	if !fuzzengine.CorpusPersistEnabled(cfg) {
		t.Fatal("deep Dig must enable corpus_persist for cross-miner reuse")
	}
	if fuzzengine.ExecPerUnit(cfg) < 64 {
		t.Fatalf("exec_per_unit=%d", fuzzengine.ExecPerUnit(cfg))
	}
	dict := fuzzengine.ParseMutatorDict(cfg)
	if len(dict) < 8 || string(dict[:4]) != "AKIA" {
		t.Fatalf("mutator_dict=%q", dict)
	}
	// Hex storage survives config_json round-trip.
	if _, ok := cfg["mutator_dict"].(string); !ok {
		t.Fatalf("mutator_dict should be hex string, got %T", cfg["mutator_dict"])
	}
	if cfg["corpus_explore_v2"] != true {
		t.Fatal("deep Dig should enable corpus_explore_v2")
	}
}

func TestApplyDigPowerSchedulingPreservesExplicitExec(t *testing.T) {
	cfg := map[string]any{"depth_tier": "bytes_corpus", "exec_per_unit": 8}
	ApplyDigPowerScheduling(cfg, "deep")
	if fuzzengine.ExecPerUnit(cfg) != 8 {
		t.Fatalf("explicit exec overwritten: %v", cfg["exec_per_unit"])
	}
}

func TestFinalizeDigCampaignConfigMergesSeeds(t *testing.T) {
	dir := t.TempDir()
	pack := "secrets"
	seedDir := DigSeedDir(dir, pack)
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "a.bin"), []byte("AKIAEXAMPLE"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := ApplyPackConfig(map[string]any{}, guardPacks[pack])
	cfg = FinalizeDigCampaignConfig(cfg, "deep", pack, dir)
	if intFromCfg(cfg, "dig_external_seeds_merged") != 1 {
		t.Fatalf("merged=%v corpus=%v", cfg["dig_external_seeds_merged"], cfg["seed_byte_corpus"])
	}
	if cfg["dig_mutator_profile"] != "secrets_supply_chain" {
		t.Fatalf("profile=%v", cfg["dig_mutator_profile"])
	}
	if cfg["dig_depth_profile"] == "" {
		t.Fatalf("missing depth profile: %+v", cfg)
	}
}

func TestDigDepthProfile(t *testing.T) {
	cfg := map[string]any{
		"depth_tier":          "wasm_native",
		"guard_pack":          "filter_utf8",
		"guided_scheduling":   true,
		"power_mut_cap":       8,
		"mutation_rounds":     6,
		"exec_per_unit":       64,
		"dig_mutator_profile": "utf8_display_filter",
	}
	got := DigDepthProfile(cfg, "audit", "filter_utf8")
	for _, want := range []string{"Dig · Audit", "pack=filter_utf8", "guided", "mut_cap=8"} {
		if !strings.Contains(got, want) {
			t.Fatalf("profile=%q missing %q", got, want)
		}
	}
}

func TestApplyDigGPUMutatorsWiresSegmentPath(t *testing.T) {
	off := map[string]any{
		"input_mode":      "bytes",
		"max_input_bytes": 128,
		"exec_per_unit":   8,
		"power_mut_cap":   8,
	}
	on := map[string]any{
		"input_mode":      "bytes",
		"max_input_bytes": 128,
		"exec_per_unit":   8,
		"power_mut_cap":   8,
	}
	ApplyDigGPUMutators(on, true)
	if !fuzzengine.DigGPUMutatorsEnabled(on) {
		t.Fatal("ApplyDigGPUMutators must set dig_gpu_mutators")
	}
	_, a := fuzzengine.SegmentExecInput(7, 3, off, nil)
	_, b := fuzzengine.SegmentExecInput(7, 3, on, nil)
	if string(a) == string(b) {
		t.Fatal("dig_gpu_mutators must change Dig segment inputs vs classic MutateBytesForConfig")
	}
}
