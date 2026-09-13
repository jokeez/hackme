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

func TestRankCorpusForCullPrefersCrashAndRare(t *testing.T) {
	seeds := []PoolCorpusSeed{
		{InputBytes: []byte("a"), Energy: 2, Edge: 1},
		{InputBytes: []byte("crash"), Energy: 2, Edge: 2, Crash: true},
		{InputBytes: []byte("rare"), Energy: 2, Edge: 3},
	}
	rarity := EdgeHitCounts{1: 50, 2: 1, 3: 1}
	rank := RankCorpusForCull(seeds, rarity)
	if !seeds[rank[0]].Crash {
		t.Fatalf("crash should rank first: %+v", rank)
	}
}
