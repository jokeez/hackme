package fuzzengine

import "testing"

func TestApplyDepthTierWasmNative(t *testing.T) {
	cfg := ApplyDepthTier(map[string]any{}, DepthWasmNative)
	if !BountyRequiresNative(cfg) {
		t.Fatal("wasm_native should require native bounty")
	}
	if !NativeReproEnabled(cfg) {
		t.Fatal("wasm_native should enable native repro")
	}
	if ParseInputMode(cfg) != InputModeUint64 {
		t.Fatalf("wasm_native input mode: %s", ParseInputMode(cfg))
	}
}

func TestApplyDepthTierBytesCorpus(t *testing.T) {
	cfg := ApplyDepthTier(map[string]any{}, DepthBytesCorpus)
	if ParseInputMode(cfg) != InputModeBytes {
		t.Fatalf("bytes corpus mode: %s", ParseInputMode(cfg))
	}
	seeds := ParseByteCorpus(cfg)
	if len(seeds) == 0 {
		t.Fatal("expected default byte seeds")
	}
}

func TestDeriveInputBytesDeterministic(t *testing.T) {
	cfg := map[string]any{
		"input_mode":        "bytes",
		"seed_byte_corpus":  []any{"0100000001", "0200000001"},
		"mutation_rounds":   2,
	}
	a := DeriveInputBytes(1, cfg)
	b := DeriveInputBytes(2, cfg)
	c := DeriveInputBytes(1, cfg)
	if string(a) == string(b) {
		t.Fatal("expected different byte inputs")
	}
	if string(a) != string(c) {
		t.Fatalf("not deterministic: %x vs %x", a, c)
	}
}

func TestPackInputBytesToU64(t *testing.T) {
	b := []byte{1, 2, 3, 0, 0, 0, 0, 0}
	u := PackInputBytesToU64(b)
	if u != 0x00030201 {
		t.Fatalf("pack: %x", u)
	}
}

func TestDepthPresetFor(t *testing.T) {
	p, ok := DepthPresetFor(DepthWasmNative)
	if !ok || p.BudgetHMC < 1 || p.BudgetRuns < 8 {
		t.Fatalf("preset: %+v ok=%v", p, ok)
	}
}
