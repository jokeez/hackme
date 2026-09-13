package fuzzengine

import (
	"strings"
	"testing"
)

func TestFindingFamilyLibuclStyle(t *testing.T) {
	msgA := `==1==ERROR: UndefinedBehaviorSanitizer: runtime error: call to function through pointer to incorrect function type
SUMMARY: UndefinedBehaviorSanitizer: function-pointer-cast ucl_hash.c:275 in ucl_parser_free`
	msgB := `==99==ERROR: UndefinedBehaviorSanitizer: runtime error: call to function through pointer to incorrect function type
SUMMARY: UndefinedBehaviorSanitizer: function-pointer-cast ucl_hash.c:275 in ucl_parser_free`
	a := FindingFamily("ubsan", msgA)
	b := FindingFamily("ubsan", msgB)
	if a != b {
		t.Fatalf("same root cause should share family: %q vs %q", a, b)
	}
	if !strings.Contains(a, "fn_ptr_cast") {
		t.Fatalf("expected fn_ptr_cast family, got %q", a)
	}
	mis := FindingFamily("ubsan", "runtime error: misaligned address 0x1 for type")
	if !strings.Contains(mis, "misaligned") {
		t.Fatalf("misaligned family: %q", mis)
	}
	if CountFindingFamilies("ubsan", []string{msgA, msgB, msgA}) != 1 {
		t.Fatal("65 inputs / 1 family style count failed")
	}
}

func TestHavocStackDeterministic(t *testing.T) {
	base := []byte(`{"a":1,"b":[2,3]}`)
	dict := []byte(`"null""true""false"{}[]`)
	corpus := [][]byte{[]byte(`{"x":0}`), []byte(`[]`), base}
	a := MutateBytesForHunt(base, StageHavocBase+8, 12345, 256, map[string]any{"mutator_dict": dict}, corpus)
	b := MutateBytesForHunt(base, StageHavocBase+8, 12345, 256, map[string]any{"mutator_dict": dict}, corpus)
	if string(a) != string(b) {
		t.Fatal("havoc stack must stay deterministic for pool replay")
	}
}

func TestCorpusExploreV2GivesLowEnergyShare(t *testing.T) {
	seeds := []PoolCorpusSeed{
		{InputBytes: []byte("hot"), Energy: 20},
		{InputBytes: []byte("cold"), Energy: 1},
	}
	classicCold := 0
	exploreCold := 0
	n := 5000
	for i := 0; i < n; i++ {
		if string(PickWeightedSeed(seeds, uint64(i)).InputBytes) == "cold" {
			classicCold++
		}
		if string(PickWeightedSeedForConfig(seeds, uint64(i), map[string]any{"corpus_explore_v2": true}).InputBytes) == "cold" {
			exploreCold++
		}
	}
	if exploreCold <= classicCold {
		t.Fatalf("explore_v2 should increase cold seed picks: classic=%d explore=%d", classicCold, exploreCold)
	}
}

func TestMeasureMutationDepthUniqueRatio(t *testing.T) {
	base := []byte(`{"a":1}`)
	dict := []byte(`"a""b""null"`)
	st := MeasureMutationDepth(base, dict, [][]byte{[]byte(`{}`), []byte(`[1]`)}, 500, 128)
	if st.UniqueSHA256 < 50 {
		t.Fatalf("expected deeper uniqueness, got %+v", st)
	}
	if st.UniqueRatio < 0.1 {
		t.Fatalf("unique_ratio too low: %+v", st)
	}
	t.Logf("depth stats: %+v", st)
}

func TestEngineABComparison(t *testing.T) {
	base := []byte(`{"a":1,"nested":{"b":[1,2,3],"tag":"<x/>"}}`)
	dict := []byte(`"null""true""false""a""nested"{}[]`)
	corpus := [][]byte{
		[]byte(`{}`), []byte(`[1,2]`), []byte(`{"x":null}`),
		[]byte(`<root><child/></root>`), base,
	}
	rep := CompareEngineAB(base, dict, corpus, 5000, 256)
	t.Logf("A/B baseline=%+v current=%+v gain_unique=%.1f%% gain_lens=%.1f%%",
		rep.Baseline, rep.Current, rep.UniqueGainPct, rep.LensGainPct)
	if rep.UniqueGainPct < 2 {
		t.Fatalf("expected >2%% havoc unique gain vs baseline, got %.1f%%", rep.UniqueGainPct)
	}
	if rep.LensGainPct < 50 {
		t.Fatalf("expected >50%% length diversity gain, got %.1f%%", rep.LensGainPct)
	}
	if rep.Current.UniqueRatio <= rep.Baseline.UniqueRatio {
		t.Fatalf("current should beat baseline unique ratio: baseline=%.3f current=%.3f",
			rep.Baseline.UniqueRatio, rep.Current.UniqueRatio)
	}
}
