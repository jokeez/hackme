package main

import (
	"testing"

	"hackme/internal/fuzzengine"
)

func TestDeriveFuzzInputUsesSeeds(t *testing.T) {
	cfg := map[string]any{
		"seed_corpus":     []any{uint64(42), uint64(99)},
		"mutation_rounds": 2,
	}
	a := deriveFuzzInput(1, cfg)
	b := deriveFuzzInput(2, cfg)
	c := deriveFuzzInput(1, cfg)
	if a == b {
		t.Fatalf("expected different inputs for different input_n, got %x", a)
	}
	if a != c {
		t.Fatalf("deriveFuzzInput not deterministic: %x vs %x", a, c)
	}
}

func TestFuzzInputSHA256Stable(t *testing.T) {
	h1 := fuzzInputSHA256(12345)
	h2 := fuzzInputSHA256(12345)
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("sha256: %q", h1)
	}
}

func TestNormalizeFuzzCampaignConfigDefaults(t *testing.T) {
	cfg := normalizeFuzzCampaignConfig(nil, "property")
	if cfg["fuzz_engine_version"] != fuzzengine.Version {
		t.Fatalf("version missing")
	}
	seeds, ok := cfg["seed_corpus"].([]any)
	if !ok || len(seeds) < 5 {
		t.Fatalf("expected default seeds, got %#v", cfg["seed_corpus"])
	}
}
