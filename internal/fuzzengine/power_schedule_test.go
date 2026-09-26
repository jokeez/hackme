package fuzzengine

import "testing"

func TestSeedScheduleWeightRarityNotBucketID(t *testing.T) {
	// Critical audit fix: high edge bucket ID must NOT imply higher weight.
	rare := PoolCorpusSeed{InputBytes: []byte("rare"), Energy: 3, Edge: 5}
	common := PoolCorpusSeed{InputBytes: []byte("common"), Energy: 3, Edge: 200}
	rarity := EdgeHitCounts{5: 1, 200: 40}
	wr := SeedScheduleWeight(rare, true, rarity)
	wc := SeedScheduleWeight(common, true, rarity)
	if wr <= wc {
		t.Fatalf("rare edge should outweigh common: rare=%d common=%d", wr, wc)
	}
}

func TestPowerScheduleDepthRareDeeper(t *testing.T) {
	dRare := PowerScheduleDepth(4, 1, 16)
	dCommon := PowerScheduleDepth(4, 50, 16)
	if dRare <= dCommon {
		t.Fatalf("rare should be deeper: rare=%d common=%d", dRare, dCommon)
	}
}

func TestSeedScheduleWeightPathRarity(t *testing.T) {
	rare := PoolCorpusSeed{InputBytes: []byte("abcdefghijklmnop"), Energy: 3, Edge: 1, Path: 7}
	common := PoolCorpusSeed{InputBytes: []byte("abcdefghijklmnop"), Energy: 3, Edge: 1, Path: 9}
	edge := EdgeHitCounts{1: 5}
	path := PathHitCounts{7: 1, 9: 40}
	wr := SeedScheduleWeightEx(rare, true, edge, path, nil)
	wc := SeedScheduleWeightEx(common, true, edge, path, nil)
	if wr <= wc {
		t.Fatalf("rare path should outweigh common: rare=%d common=%d", wr, wc)
	}
}

func TestSeedScheduleWeightLengthClassMidRare(t *testing.T) {
	mid := PoolCorpusSeed{InputBytes: make([]byte, 32), Energy: 3, Edge: 1}
	tiny := PoolCorpusSeed{InputBytes: []byte("x"), Energy: 3, Edge: 1}
	edge := EdgeHitCounts{1: 5}
	lens := LengthClassHits{1: 20, 2: 1, 3: 10}
	wm := SeedScheduleWeightEx(mid, true, edge, nil, lens)
	wt := SeedScheduleWeightEx(tiny, true, edge, nil, lens)
	if wm <= wt {
		t.Fatalf("rare mid-len should outweigh common tiny: mid=%d tiny=%d", wm, wt)
	}
}

func TestPowerScheduleDepthExPathAdds(t *testing.T) {
	base := PowerScheduleDepthEx(4, 10, 0, 0, 16)
	withPath := PowerScheduleDepthEx(4, 10, 1, 0, 16)
	if withPath <= base {
		t.Fatalf("path rarity should deepen: base=%d with=%d", base, withPath)
	}
}

func TestHangObserveBoostSeparateFromCrash(t *testing.T) {
	if !IsHangOnly("timeout_hang") || !IsHangOnly("hang") {
		t.Fatal("expected hang-only types")
	}
	if IsHangOnly("asan_timeout") {
		t.Fatal("sanitizer+timeout must not be hang-only")
	}
	crashBoost := CorpusObserveBoostEx(true, false, false, false)
	hangBoost := CorpusObserveBoostEx(true, true, false, false)
	if hangBoost >= crashBoost {
		t.Fatalf("hang boost must be softer than crash: hang=%d crash=%d", hangBoost, crashBoost)
	}
	// Hang must not get crash floor on flat observe.
	flatHang := ApplyObserveEnergyEx(3, 1, false, true, false, false, false)
	if flatHang < 2 {
		t.Fatalf("hang floor should be >=2, got %d", flatHang)
	}
	flatCrash := ApplyObserveEnergyEx(3, 1, true, false, false, false, false)
	if flatCrash < 3 {
		t.Fatalf("crash floor should hold, got %d", flatCrash)
	}
}

func TestHavocOpPickDeterministic(t *testing.T) {
	a := havocOpPick(0xDEADBEEF)
	b := havocOpPick(0xDEADBEEF)
	if a != b || a < 0 || a >= HavocOpModulo {
		t.Fatalf("havocOpPick unstable: %d %d", a, b)
	}
	seen := map[int]bool{}
	for i := 0; i < 2000; i++ {
		seen[havocOpPick(uint64(i)*0x9E3779B97F4A7C15)] = true
	}
	if len(seen) < 40 {
		t.Fatalf("weight table should still reach most ops, got %d unique", len(seen))
	}
}

func TestCrossoverBytesTwoPointDeterministic(t *testing.T) {
	a := []byte("abcdefghijklmnopqrstuvwxyz")
	b := []byte("0123456789ABCDEFGHIJKLMNOP")
	x := crossoverBytesTwoPoint(a, b, 0xC0FFEE, 64)
	y := crossoverBytesTwoPoint(a, b, 0xC0FFEE, 64)
	if string(x) != string(y) || len(x) == 0 {
		t.Fatalf("two-point splice unstable: %q vs %q", x, y)
	}
}

func TestDecayEnergyCoolsStale(t *testing.T) {
	if DecayEnergy(10, false, false, false, false) >= 10 {
		t.Fatal("expected decay")
	}
	if DecayEnergy(3, true, false, false, false) < 3 {
		t.Fatal("crash floor")
	}
	got := ApplyObserveEnergy(5, 3, false, true, false, false)
	if got < 8 {
		t.Fatalf("novelty should boost, got %d", got)
	}
	flat := ApplyObserveEnergy(8, 1, false, false, false, false)
	if flat >= 8 {
		t.Fatalf("flat observe should decay, got %d", flat)
	}
}

func TestCompactCorpusSeed(t *testing.T) {
	in := []byte{'a', 'b', 0, 0, 0}
	out := CompactCorpusSeed(in, 0)
	if len(out) != 5 || out[4] != 0 {
		t.Fatalf("must preserve trailing NULs: %q", out)
	}
	trimmed := CompactCorpusSeed(in, 3)
	if len(trimmed) != 3 {
		t.Fatalf("maxLen clamp: %q", trimmed)
	}
}

func TestMeasureGuidedDiversity(t *testing.T) {
	cfg := map[string]any{
		"input_mode":           "bytes",
		"corpus_explore_v2":    true,
		"coverage_feedback_v1": true,
		"guided_scheduling":    true,
		"power_mut_cap":        12,
	}
	seeds := []PoolCorpusSeed{
		{InputBytes: []byte(`{"a":1}`), Energy: 4, Edge: 10},
		{InputBytes: []byte(`{"b":[2]}`), Energy: 2, Edge: 20},
		{InputBytes: []byte(`<x/>`), Energy: 3, Edge: 30},
	}
	st := MeasureGuidedDiversity(cfg, seeds, 1000)
	t.Logf("guided diversity: %+v", st)
	if st.UniqueRatio < 0.15 {
		t.Fatalf("expected decent uniqueness, got %+v", st)
	}
	if st.UniqueSHA256 < 100 {
		t.Fatalf("too few unique guided inputs: %+v", st)
	}
}

func TestCullCorpusKeepRareAndCrash(t *testing.T) {
	seeds := []PoolCorpusSeed{
		{InputBytes: []byte("common-a"), Energy: 8, Edge: 1},
		{InputBytes: []byte("common-b"), Energy: 8, Edge: 1},
		{InputBytes: []byte("common-c"), Energy: 8, Edge: 1},
		{InputBytes: []byte("rare"), Energy: 1, Edge: 99},
		{InputBytes: []byte("crash"), Energy: 1, Edge: 2, Crash: true},
		{InputBytes: []byte("filler"), Energy: 9, Edge: 3},
	}
	rarity := BuildEdgeHitCounts(seeds)
	kept := CullCorpusKeep(seeds, rarity, 3)
	if len(kept) != 3 {
		t.Fatalf("want 3 kept, got %d", len(kept))
	}
	hasCrash, hasRare := false, false
	for _, s := range kept {
		if s.Crash {
			hasCrash = true
		}
		if string(s.InputBytes) == "rare" {
			hasRare = true
		}
	}
	if !hasCrash || !hasRare {
		t.Fatalf("must keep crash+rare under cull: %+v", kept)
	}
}

func TestAutodictFrequencyPrefersRepeated(t *testing.T) {
	toks := ExtractAutodictTokens(
		[]byte(`{"userId":1}`),
		[]byte(`{"userId":2}`),
		[]byte(`{"userId":3,"other":9}`),
	)
	if len(toks) == 0 {
		t.Fatal("expected tokens")
	}
	if string(toks[0]) != "userId" && !containsTok(toks, "userId") {
		t.Fatalf("expected userId in autodict, got %v", toks)
	}
}

func containsTok(toks [][]byte, want string) bool {
	for _, t := range toks {
		if string(t) == want {
			return true
		}
	}
	return false
}
