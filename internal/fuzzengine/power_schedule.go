package fuzzengine

import "crypto/sha256"

// EdgeHitCounts maps edge_bucket → how many corpus seeds share that edge.
// Used for rarity-aware scheduling (low hit count = rare = higher weight).
type EdgeHitCounts map[int]int

// PathHitCounts maps path_bucket → how many corpus seeds share that path (v2.9).
type PathHitCounts map[int]int

// LengthClassHits maps length-class id → how many seeds fall in that class (v2.9).
type LengthClassHits map[int]int

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

// BuildPathHitCounts counts how many corpus seeds share each path bucket.
func BuildPathHitCounts(seeds []PoolCorpusSeed) PathHitCounts {
	if len(seeds) == 0 {
		return nil
	}
	out := PathHitCounts{}
	for _, s := range seeds {
		if s.Path > 0 {
			out[s.Path]++
		}
	}
	return out
}

// LengthClass buckets input length for AFL-ish mid-len preference.
// 1=tiny 2=short-mid 3=mid 4=large 5=huge (0 = empty).
func LengthClass(n int) int {
	switch {
	case n <= 0:
		return 0
	case n <= 15:
		return 1
	case n <= 64:
		return 2
	case n <= 256:
		return 3
	case n <= 1024:
		return 4
	default:
		return 5
	}
}

// LengthClassMid reports whether the class is the preferred mid band.
func LengthClassMid(class int) bool {
	return class == 2 || class == 3
}

// BuildLengthClassHitCounts counts seeds per length class.
func BuildLengthClassHitCounts(seeds []PoolCorpusSeed) LengthClassHits {
	if len(seeds) == 0 {
		return nil
	}
	out := LengthClassHits{}
	for _, s := range seeds {
		c := LengthClass(len(s.InputBytes))
		if c > 0 {
			out[c]++
		}
	}
	return out
}

// SeedScheduleWeight returns AFL-ish weight for one corpus seed.
// exploreV2=false → classic energy².
// rarity: optional edge hit counts; rare edges (low hits) get a boost.
// Path/length rarity are derived when SeedScheduleWeightEx is used from the pool picker.
func SeedScheduleWeight(s PoolCorpusSeed, exploreV2 bool, rarity EdgeHitCounts) int {
	return SeedScheduleWeightEx(s, exploreV2, rarity, nil, nil)
}

// SeedScheduleWeightEx adds path-hit rarity and mid-len length-class energy (v2.9).
func SeedScheduleWeightEx(s PoolCorpusSeed, exploreV2 bool, edge EdgeHitCounts, path PathHitCounts, lens LengthClassHits) int {
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
		w += 18 // v2.8: crash seeds get stronger fleet pull
	}
	if s.Energy >= 8 {
		w += 4
	}
	if s.Energy >= 16 {
		w += 4
	}
	// Rarity: prefer seeds whose structural edge is rarely seen (not high bucket ID!).
	if edge != nil && s.Edge > 0 {
		hits := edge[s.Edge]
		switch {
		case hits <= 1:
			w += 36 // singleton edge — AFL rare-bitmap energy
		case hits <= 2:
			w += 24
		case hits <= 4:
			w += 14
		case hits <= 8:
			w += 8
		case hits <= 20:
			w += 3
		}
	} else if s.Edge > 0 {
		// No rarity map — small fixed novelty hint only (edge present).
		w += 2
	}
	// v2.9: path-hit rarity mirrors edge (slightly softer tiers).
	if path != nil && s.Path > 0 {
		hits := path[s.Path]
		switch {
		case hits <= 1:
			w += 18
		case hits <= 2:
			w += 12
		case hits <= 4:
			w += 7
		case hits <= 8:
			w += 4
		case hits <= 20:
			w += 2
		}
	} else if s.Path > 0 {
		w += 2
	}
	// v2.9: length-class energy — prefer rare mid-len seeds.
	if lens != nil {
		lc := LengthClass(len(s.InputBytes))
		if lc > 0 {
			hits := lens[lc]
			if LengthClassMid(lc) {
				switch {
				case hits <= 1:
					w += 14
				case hits <= 2:
					w += 10
				case hits <= 4:
					w += 6
				case hits <= 8:
					w += 3
				}
			} else if hits <= 1 {
				w += 3 // rare non-mid still gets a small novelty nudge
			}
		}
	}
	return w
}

// PowerScheduleDepth returns how many havoc-style mutation stages to apply for one seed.
// Higher energy + rarer edges → deeper mutation stack (still capped for replay cost).
func PowerScheduleDepth(energy, edgeHits, cap int) int {
	return PowerScheduleDepthEx(energy, edgeHits, 0, 0, cap)
}

// PowerScheduleDepthEx adds path-hit and length-class depth (v2.9).
func PowerScheduleDepthEx(energy, edgeHits, pathHits, lenClassHits, cap int) int {
	if cap < 1 {
		cap = 4
	}
	if cap > 36 {
		cap = 36
	}
	depth := 1 + energy/2
	if edgeHits <= 1 {
		depth += 6
	} else if edgeHits <= 2 {
		depth += 4
	} else if edgeHits <= 4 {
		depth += 3
	} else if edgeHits <= 12 {
		depth += 1
	}
	// Path rarity — softer than edge.
	if pathHits > 0 {
		if pathHits <= 1 {
			depth += 3
		} else if pathHits <= 2 {
			depth += 2
		} else if pathHits <= 4 {
			depth += 1
		}
	}
	// Length-class: mid-band rare preferred (lenClassHits is hits for this seed's class;
	// negative sentinel unused — callers pass 0 when unknown).
	if lenClassHits > 0 {
		if lenClassHits <= 1 {
			depth += 2
		} else if lenClassHits <= 3 {
			depth += 1
		}
	}
	if energy >= 12 {
		depth += 2
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
	return PowerScheduleStageEx(inputN, energy, edgeHits, 0, 0, cap)
}

// PowerScheduleStageEx includes path + length-class rarity in stage selection (v2.9).
func PowerScheduleStageEx(inputN uint64, energy, edgeHits, pathHits, lenClassHits, cap int) MutationStage {
	depth := PowerScheduleDepthEx(energy, edgeHits, pathHits, lenClassHits, cap)
	stageCount := StageDeterministicMax + depth
	mutIdx := int(inputN % uint64(MutationsForSeedCapped(energy, depth)))
	return MutationStage((int(inputN) + mutIdx*17 + energy + edgeHits*3 + pathHits*2 + lenClassHits) % stageCount)
}

// DecayEnergy soft-decays seed energy when an observe produced no novelty.
// Crash seeds never decay below 4; hang seeds floor at 2; others floor at 1.
func DecayEnergy(energy int, crash, newEdge, newPath, recordFinding bool) int {
	return DecayEnergyEx(energy, crash, false, newEdge, newPath, recordFinding)
}

// DecayEnergyEx separates hang/timeout floor from crash floor (v2.9).
func DecayEnergyEx(energy int, crash, hangOnly, newEdge, newPath, recordFinding bool) int {
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
	} else if hangOnly && next < 2 {
		next = 2
	}
	if next < 1 {
		next = 1
	}
	return next
}

// ApplyObserveEnergy merges boost into current energy, then optionally decays on flat observes.
func ApplyObserveEnergy(current, boost int, crash, newEdge, newPath, recordFinding bool) int {
	return ApplyObserveEnergyEx(current, boost, crash, false, newEdge, newPath, recordFinding)
}

// ApplyObserveEnergyEx treats hang/timeout findings as non-crash for floor/boost wiring (v2.9).
func ApplyObserveEnergyEx(current, boost int, crash, hangOnly, newEdge, newPath, recordFinding bool) int {
	if current < 1 {
		current = 1
	}
	if boost < 0 {
		boost = 0
	}
	if hangOnly {
		crash = false // hang must not inherit crash energy floor
	}
	if newEdge || newPath || recordFinding {
		out := current + boost
		if out > 64 {
			out = 64
		}
		return out
	}
	// No novelty: slight decay so the fleet rotates.
	return DecayEnergyEx(current, crash, hangOnly, false, false, false)
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
// Prefer crash > rare singleton edges > energy/weight > shorter compact length.
func RankCorpusForCull(seeds []PoolCorpusSeed, rarity EdgeHitCounts) []int {
	idx := make([]int, len(seeds))
	for i := range seeds {
		idx[i] = i
	}
	path := BuildPathHitCounts(seeds)
	lens := BuildLengthClassHitCounts(seeds)
	// Stable insertion sort — small corpus (≤4096), keep deps zero.
	for i := 1; i < len(idx); i++ {
		j := i
		for j > 0 && corpusBetterEx(seeds[idx[j]], seeds[idx[j-1]], rarity, path, lens) {
			idx[j], idx[j-1] = idx[j-1], idx[j]
			j--
		}
	}
	return idx
}

// CullCorpusKeep returns up to max seeds, always retaining crashes and singleton-edge seeds
// when possible (AFL-ish rare-edge + crash preservation under pool_corpus_max).
func CullCorpusKeep(seeds []PoolCorpusSeed, rarity EdgeHitCounts, max int) []PoolCorpusSeed {
	if max < 1 || len(seeds) == 0 {
		return nil
	}
	if len(seeds) <= max {
		out := make([]PoolCorpusSeed, len(seeds))
		copy(out, seeds)
		return out
	}
	if rarity == nil {
		rarity = BuildEdgeHitCounts(seeds)
	}
	rank := RankCorpusForCull(seeds, rarity)
	kept := make([]PoolCorpusSeed, 0, max)
	seen := map[int]struct{}{}

	// Pass 1: mandatory crashes.
	for _, i := range rank {
		if len(kept) >= max {
			break
		}
		if !seeds[i].Crash {
			continue
		}
		kept = append(kept, seeds[i])
		seen[i] = struct{}{}
	}
	// Pass 2: singleton (hits<=1) rare edges not yet kept.
	for _, i := range rank {
		if len(kept) >= max {
			break
		}
		if _, ok := seen[i]; ok {
			continue
		}
		if seeds[i].Edge > 0 && rarity[seeds[i].Edge] <= 1 {
			kept = append(kept, seeds[i])
			seen[i] = struct{}{}
		}
	}
	// Pass 2b (v2.9): singleton rare paths.
	path := BuildPathHitCounts(seeds)
	for _, i := range rank {
		if len(kept) >= max {
			break
		}
		if _, ok := seen[i]; ok {
			continue
		}
		if seeds[i].Path > 0 && path != nil && path[seeds[i].Path] <= 1 {
			kept = append(kept, seeds[i])
			seen[i] = struct{}{}
		}
	}
	// Pass 3: fill by rank.
	for _, i := range rank {
		if len(kept) >= max {
			break
		}
		if _, ok := seen[i]; ok {
			continue
		}
		kept = append(kept, seeds[i])
		seen[i] = struct{}{}
	}
	return kept
}

func corpusBetter(a, b PoolCorpusSeed, rarity EdgeHitCounts) bool {
	return corpusBetterEx(a, b, rarity, nil, nil)
}

func corpusBetterEx(a, b PoolCorpusSeed, edge EdgeHitCounts, path PathHitCounts, lens LengthClassHits) bool {
	if a.Crash != b.Crash {
		return a.Crash
	}
	// Prefer singleton rare edges before weight so cull keeps coverage diversity.
	if edge != nil {
		ha, hb := 9999, 9999
		if a.Edge > 0 {
			ha = edge[a.Edge]
		}
		if b.Edge > 0 {
			hb = edge[b.Edge]
		}
		ra, rb := ha <= 1, hb <= 1
		if ra != rb {
			return ra
		}
		if ha != hb && (ha <= 3 || hb <= 3) {
			return ha < hb
		}
	}
	wa := SeedScheduleWeightEx(a, true, edge, path, lens)
	wb := SeedScheduleWeightEx(b, true, edge, path, lens)
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
