package fuzzengine

import "testing"

func TestDeriveInputUsesSeeds(t *testing.T) {
	cfg := map[string]any{
		"seed_corpus":     []any{uint64(42), uint64(99)},
		"mutation_rounds": 2,
	}
	a := DeriveInput(1, cfg)
	b := DeriveInput(2, cfg)
	c := DeriveInput(1, cfg)
	if a == b {
		t.Fatalf("expected different inputs for different input_n, got %x", a)
	}
	if a != c {
		t.Fatalf("DeriveInput not deterministic: %x vs %x", a, c)
	}
}

func TestInputSHA256Stable(t *testing.T) {
	h1 := InputSHA256(12345)
	h2 := InputSHA256(12345)
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("sha256: %q", h1)
	}
}

func TestNormalizeCampaignConfigDefaults(t *testing.T) {
	cfg := NormalizeCampaignConfig(nil, "property")
	if cfg["fuzz_engine_version"] != Version {
		t.Fatalf("version missing")
	}
	if cfg["check_semantics"] != "detector" {
		t.Fatalf("property campaigns should default to detector semantics, got %#v", cfg["check_semantics"])
	}
	seeds, ok := cfg["seed_corpus"].([]any)
	if !ok || len(seeds) < 5 {
		t.Fatalf("expected default seeds, got %#v", cfg["seed_corpus"])
	}
}

func TestMetaFromConfigStableBuckets(t *testing.T) {
	meta := MetaFromConfig(map[string]any{"check_semantics": "detector"})
	found := false
	for _, f := range meta["features"].([]string) {
		if f == "stable_crash_buckets" {
			found = true
		}
	}
	if !found {
		t.Fatal("expected stable_crash_buckets feature")
	}
}

func TestGuidedSchedulingFlag(t *testing.T) {
	if GuidedSchedulingEnabled(map[string]any{"guided_scheduling": true}) != true {
		t.Fatal("expected enabled")
	}
	if GuidedSchedulingEnabled(map[string]any{}) {
		t.Fatal("expected disabled by default")
	}
}

func TestEvalCheckDetector(t *testing.T) {
	pass, finding := EvalCheck(SemanticsDetector, 1, nil)
	if pass || !finding {
		t.Fatalf("detector: violation should be finding")
	}
	pass, finding = EvalCheck(SemanticsDetector, 0, nil)
	if !pass || finding {
		t.Fatalf("detector: clean should pass")
	}
}

func TestEvalCheckPoWGate(t *testing.T) {
	pass, finding := EvalCheck(SemanticsPoWGate, 1, nil)
	if !pass || finding {
		t.Fatalf("pow_gate: non-zero should pass")
	}
	pass, finding = EvalCheck(SemanticsPoWGate, 0, nil)
	if pass || !finding {
		t.Fatalf("pow_gate: zero should finding")
	}
}
