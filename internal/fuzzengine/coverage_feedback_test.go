package fuzzengine

import "testing"

func TestCoverageBucketsStructuralMoreGranular(t *testing.T) {
	a := []byte(`{"a":1}`)
	b := []byte(`{"a":1,"b":[2,3]}`)
	e1, p1 := CoverageBucketsFromBytes(a)
	e2, p2 := CoverageBucketsFromBytes(b)
	se1, sp1 := CoverageBucketsStructural(a)
	se2, sp2 := CoverageBucketsStructural(b)
	if e1 == e2 && p1 == p2 {
		t.Fatal("fingerprint buckets should differ for different JSON")
	}
	if se1 == se2 && sp1 == sp2 {
		t.Fatal("structural buckets should differ")
	}
}

func TestCoverageFeedbackBoost(t *testing.T) {
	cfg := map[string]any{"coverage_feedback_v1": true}
	b1 := CorpusObserveBoostWithCoverage(cfg, false, true, false, nil)
	b2 := CorpusObserveBoostWithCoverage(cfg, false, true, true, nil)
	if b2 <= b1 {
		t.Fatalf("dual novelty should boost more: b1=%d b2=%d", b1, b2)
	}
	bitmap := make([]byte, 256)
	bitmap[1] = 1
	bitmap[42] = 3
	bitmap[100] = 2
	b3 := CorpusObserveBoostWithCoverage(cfg, false, true, true, bitmap)
	if b3 <= b2 {
		t.Fatalf("bitmap signal should add boost: b2=%d b3=%d", b2, b3)
	}
}

func TestCoverageFeedbackSeedWeight(t *testing.T) {
	// Equal energy: rare edge (hits=1) must beat common edge (hits=40).
	seeds := []PoolCorpusSeed{
		{InputBytes: []byte("common"), Energy: 2, Edge: 10, Path: 1},
		{InputBytes: []byte("rare"), Energy: 2, Edge: 99, Path: 1},
	}
	cfg := map[string]any{"coverage_feedback_v1": true}
	rarity := EdgeHitCounts{10: 40, 99: 1}
	rarePicks := 0
	for i := 0; i < 3000; i++ {
		if string(PickWeightedSeedWithRarity(seeds, uint64(i), cfg, rarity).InputBytes) == "rare" {
			rarePicks++
		}
	}
	if rarePicks < 1500 {
		t.Fatalf("rare edge should dominate, got %d/3000", rarePicks)
	}
}
