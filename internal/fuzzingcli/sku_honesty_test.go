package fuzzingcli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/fuzzengine"
)

func TestDigProductMode(t *testing.T) {
	if DigProductMode("scan") != "smoke" || DigProductMode("audit") != "smoke" {
		t.Fatal("scan/audit must be smoke")
	}
	if DigProductMode("deep") != "deep" {
		t.Fatal("deep must be deep")
	}
}

func TestBuildDigSKUHonestyExposesReplayAndPromise(t *testing.T) {
	t.Setenv("HACKME_POOL_REPLAY_SAMPLE_PCT", "5")
	cfg := map[string]any{
		"dig_package":               "audit",
		"depth_tier":                "wasm_native",
		"dig_external_seeds_merged": 0,
	}
	h := BuildDigSKUHonesty(cfg, 64, 64, 256, false)
	if h.ProductMode != "smoke" || h.ReplayPolicy != "sampled" || h.ReplaySamplePct != 5 {
		t.Fatalf("%+v", h)
	}
	if h.SeedsMerged {
		t.Fatal("smoke must not claim seeds merged")
	}
	if h.PromiseNote == "" || !strings.Contains(h.PromiseNote, "OSS-Fuzz") {
		t.Fatalf("promise=%q", h.PromiseNote)
	}
}

func TestFinalizeDeepMergesSeedsAuditDoesNot(t *testing.T) {
	dir := t.TempDir()
	pack := "secrets"
	seedDir := DigSeedDir(dir, pack)
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "a.bin"), []byte("AKIAEXAMPLE"), 0o644); err != nil {
		t.Fatal(err)
	}
	audit := ApplyPackConfig(map[string]any{}, guardPacks[pack])
	audit = FinalizeDigCampaignConfig(audit, "audit", pack, dir)
	if intFromCfg(audit, "dig_external_seeds_merged") != 0 {
		t.Fatalf("audit must not merge seeds, got %v", audit["dig_external_seeds_merged"])
	}
	if DigProductMode(cfgString(audit, "dig_package")) != "smoke" {
		t.Fatalf("product_mode dig_package=%v", audit["dig_package"])
	}
	deep := ApplyPackConfig(map[string]any{}, guardPacks[pack])
	deep = FinalizeDigCampaignConfig(deep, "deep", pack, dir)
	if intFromCfg(deep, "dig_external_seeds_merged") != 1 {
		t.Fatalf("deep must merge seeds, got %v", deep["dig_external_seeds_merged"])
	}
	if cfgString(deep, "product_mode") != "deep" {
		t.Fatalf("product_mode=%v", deep["product_mode"])
	}
	_ = fuzzengine.ParseDepthTier(deep)
}
