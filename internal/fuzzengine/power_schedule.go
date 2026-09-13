package fuzzengine

import "crypto/sha256"

// EdgeHitCounts maps edge_bucket → how many corpus seeds share that edge.
// Used for rarity-aware scheduling (low hit count = rare = higher weight).
type EdgeHitCounts map[int]int

// BuildEdgeHitCounts counts how many corpus seeds share each edge bucket.
func BuildEdgeHitCounts(seeds []PoolCorpusSeed) EdgeHitCounts {
	if len(seeds) == 0 {
		return nil
	}
	out := EdgeHitCounts{}
	for _, s := range seeds {
		if s.Edge > 0 {
			out[s.Edge]++
		}
	}
	return out
}

// SeedScheduleWeight returns AFL-ish weight for one corpus seed.
// exploreV2=false → classic energy².
// rarity: optional edge hit counts; rare edges (low hits) get a boost.
func SeedScheduleWeight(s PoolCorpusSeed, exploreV2 bool, rarity EdgeHitCounts) int {
	if !exploreV2 {
		w := s.Energy * s.Energy
		if w < 1 {
			w = 1
		}
		return w
	}
	e := s.Energy + 1
	if e < 1 {
		e = 1
	}
	w := e * e
	if w < 4 {
		w = 4
	}
	if !s.Crash && s.Energy < 3 {
		w += 5
	}
	if s.Crash {
		w += 12
	}
	if s.Energy >= 8 {
		w += 3
	}
	// Rarity: prefer seeds whose structural edge is rarely seen (not high bucket ID!).
	if rarity != nil && s.Edge > 0 {
		hits := rarity[s.Edge]
		switch {
		case hits <= 1:
			w += 24
		case hits <= 3:
			w += 12
		case hits <= 8:
			w += 6
		case hits <= 20:
			w += 2
		}
	} else if s.Edge > 0 {
		// No rarity map — small fixed novelty hint only (edge present).
		w += 2
	}
	if s.Path > 0 {
		w += 1
	}
	return w
}

// PowerScheduleDepth returns how many havoc-style mutation stages to apply for one seed.
// Higher energy + rarer edges → deeper mutation stack (still capped for replay cost).
func PowerScheduleDepth(energy, edgeHits, cap int) int {
	if cap < 1 {
		cap = 4
	}
	if cap > 32 {
		cap = 32
	}
	depth := 1 + energy/2
	if edgeHits <= 1 {
		depth += 4
	} else if edgeHits <= 4 {
		depth += 2
	} else if edgeHits <= 12 {
		depth += 1
	}
	if depth < 1 {
		depth = 1
	}
	if depth > cap {
		depth = cap
	}
	return depth
}

// PowerScheduleStage picks a MutationStage from inputN + seed energy + rarity.
func PowerScheduleStage(inputN uint64, energy, edgeHits, cap int) MutationStage {
	depth := PowerScheduleDepth(energy, edgeHits, cap)
	stageCount := StageDeterministicMax + depth
	mutIdx := int(inputN % uint64(MutationsForSeedCapped(energy, depth)))
	return MutationStage((int(inputN) + mutIdx*17 + energy + edgeHits*3) % stageCount)
}

// DecayEnergy soft-decays seed energy when an observe produced no novelty.
// Crash seeds never decay below 4; others floor at 1.
func DecayEnergy(energy int, crash, newEdge, newPath, recordFinding bool) int {
	if recordFinding || newEdge || newPath {
		return energy // novelty path uses boost separately
	}
	if energy <= 1 {
		return 1
	}
	next := energy - 1
	if energy >= 10 {
		next = energy - 2 // hot seeds cool faster when stale
	}
	if crash && next < 4 {
		next = 4
	}
	if next < 1 {
		next = 1
	}
	return next
}

// ApplyObserveEnergy merges boost into current energy, then optionally decays on flat observes.
func ApplyObserveEnergy(current, boost int, crash, newEdge, newPath, recordFinding bool) int {
	if current < 1 {
		current = 1
	}
	if boost < 0 {
		boost = 0
	}
	if newEdge || newPath || recordFinding {
		out := current + boost
		if out > 64 {
			out = 64
		}
		return out
	}
	// No novelty: slight decay so the fleet rotates.
	return DecayEnergy(current, crash, false, false, false)
}

// CorpusDiversityStats summarizes whether guided inputs waste cycles on duplicates.
type CorpusDiversityStats struct {
	Samples      int     `json:"samples"`
	UniqueSHA256 int     `json:"unique_sha256"`
	UniqueRatio  float64 `json:"unique_ratio"`
	WasteRatio   float64 `json:"waste_ratio"` // 1 - unique_ratio
	UniqueSeeds  int     `json:"unique_parent_seeds"`
}

// MeasureGuidedDiversity runs GuidedInputForWork over inputN range and reports uniqueness.
func MeasureGuidedDiversity(cfg map[string]any, seeds []PoolCorpusSeed, samples int) CorpusDiversityStats {
	if samples < 1 {
		samples = 1
	}
	seen := map[string]struct{}{}
	parents := map[string]struct{}{}
	for i := 0; i < samples; i++ {
		_, b := GuidedInputForWork(uint64(i), cfg, seeds)
		sum := sha256.Sum256(b)
		seen[string(sum[:])] = struct{}{}
	}
	for _, s := range seeds {
		if len(s.InputBytes) > 0 {
			sum := sha256.Sum256(s.InputBytes)
			parents[string(sum[:])] = struct{}{}
		}
	}
	st := CorpusDiversityStats{
		Samples:      samples,
		UniqueSHA256: len(seen),
		UniqueSeeds:  len(parents),
	}
	if samples > 0 {
		st.UniqueRatio = float64(st.UniqueSHA256) / float64(samples)
		st.WasteRatio = 1 - st.UniqueRatio
	}
	return st
}

// CompactCorpusSeed clamps to maxLen only.
// It deliberately does NOT strip trailing 0x00 — those bytes are often meaningful
// for binary parsers and Hunt ASAN stdin replay must stay bit-identical.
func CompactCorpusSeed(b []byte, maxLen int) []byte {
	if len(b) == 0 {
		return b
	}
	out := append([]byte(nil), b...)
	if maxLen > 0 && len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}

// RankCorpusForCull returns indices ordered best-first for keeping under pool_corpus_max.
// Prefer crash > energy > rare edge > shorter compact length.
func RankCorpusForCull(seeds []PoolCorpusSeed, rarity EdgeHitCounts) []int {
	idx := make([]int, len(seeds))
	for i := range seeds {
		idx[i] = i
	}
	// Stable insertion sort — small corpus (≤4096), keep deps zero.
	for i := 1; i < len(idx); i++ {
		j := i
		for j > 0 && corpusBetter(seeds[idx[j]], seeds[idx[j-1]], rarity) {
			idx[j], idx[j-1] = idx[j-1], idx[j]
			j--
		}
	}
	return idx
}

func corpusBetter(a, b PoolCorpusSeed, rarity EdgeHitCounts) bool {
	if a.Crash != b.Crash {
		return a.Crash
	}
	wa := SeedScheduleWeight(a, true, rarity)
	wb := SeedScheduleWeight(b, true, rarity)
	if wa != wb {
		return wa > wb
	}
	la, lb := len(a.InputBytes), len(b.InputBytes)
	if la == 0 {
		la = 8
	}
	if lb == 0 {
		lb = 8
	}
	return la < lb // prefer compact
}
