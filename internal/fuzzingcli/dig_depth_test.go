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
	cfg = FinalizeDigCampaignConfig(cfg, "audit", pack, dir)
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
		"depth_tier":         "wasm_native",
		"guard_pack":         "filter_utf8",
		"guided_scheduling":  true,
		"power_mut_cap":      8,
		"mutation_rounds":    6,
		"exec_per_unit":      64,
		"dig_mutator_profile": "utf8_display_filter",
	}
	got := DigDepthProfile(cfg, "audit", "filter_utf8")
	for _, want := range []string{"Dig · Audit", "pack=filter_utf8", "guided", "mut_cap=8"} {
		if !strings.Contains(got, want) {
			t.Fatalf("profile=%q missing %q", got, want)
		}
	}
}
