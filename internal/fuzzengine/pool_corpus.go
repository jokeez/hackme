package fuzzengine

import "strings"

// PoolCorpusSeed is a scheduling entry for guided pool work.
type PoolCorpusSeed struct {
	Input      uint64
	InputBytes []byte // non-empty for byte-mode corpus entries
	Energy     int
	Edge       int
	Path       int
	Crash      bool
}

// PoolCorpusMax returns max explorer seeds kept per campaign (guided pilot default 256).
func PoolCorpusMax(cfg map[string]any) int {
	if cfg == nil {
		return 256
	}
	if v, ok := cfg["pool_corpus_max"]; ok {
		n := intFromAny(v)
		if n >= 16 && n <= 4096 {
			return n
		}
	}
	return 256
}

// DefaultPowerMutCap returns tier-default mutation depth for pool segments.
func DefaultPowerMutCap(tier DepthTier) int {
	switch tier {
	case DepthWasmOnly:
		return 2
	case DepthWasmNative:
		return 6
	case DepthBytesCorpus, DepthUpstreamBinary, DepthOSSCVE:
		return 12
	default:
		return 4
	}
}

// PowerMutCap caps mutations per lab-style run; pool uses one mutation per work item.
func PowerMutCap(cfg map[string]any) int {
	if cfg == nil {
		return 4
	}
	if v, ok := cfg["power_mut_cap"]; ok {
		n := intFromAny(v)
		if n >= 1 && n <= 32 {
			return n
		}
	}
	return DefaultPowerMutCap(ParseDepthTier(cfg))
}

// CorpusExploreV2Enabled turns on explore_v2 seed weighting (opt-in; classic remains default for replay safety).
func CorpusExploreV2Enabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	v, ok := cfg["corpus_explore_v2"]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case int:
		return t != 0
	case float64:
		return t != 0
	default:
		return false
	}
}

// PickWeightedSeed selects a corpus seed deterministically from inputN (anti-cheat stable at claim).
func PickWeightedSeed(seeds []PoolCorpusSeed, inputN uint64) PoolCorpusSeed {
	return pickWeightedSeedMode(seeds, inputN, false)
}

// PickWeightedSeedForConfig uses classic weights, or explore_v2 when cfg enables it.
func PickWeightedSeedForConfig(seeds []PoolCorpusSeed, inputN uint64, cfg map[string]any) PoolCorpusSeed {
	return pickWeightedSeedMode(seeds, inputN, CorpusExploreV2Enabled(cfg))
}

func pickWeightedSeedMode(seeds []PoolCorpusSeed, inputN uint64, exploreV2 bool) PoolCorpusSeed {
	if len(seeds) == 0 {
		return PoolCorpusSeed{}
	}
	weights := make([]int, len(seeds))
	total := 0
	for i, s := range seeds {
		var w int
		if exploreV2 {
			// (energy+1)^2 keeps high-energy preferred but gives low-energy a floor share
			// so the fleet does not collapse onto one seed.
			e := s.Energy + 1
			if e < 1 {
				e = 1
			}
			w = e * e
			if !s.Crash && s.Energy < 3 {
				w += 3
			}
			if s.Edge > 0 {
				w += 1
			}
		} else {
			w = s.Energy * s.Energy
			if w < 1 {
				w = 1
			}
		}
		weights[i] = w
		total += w
	}
	if total <= 0 {
		return seeds[int(inputN)%len(seeds)]
	}
	pick := int(inputN % uint64(total))
	acc := 0
	for i, w := range weights {
		acc += w
		if pick < acc {
			return seeds[i]
		}
	}
	return seeds[len(seeds)-1]
}

// GuidedInputForWork derives the single pool input for a work item from corpus + inputN.
func GuidedInputForWork(inputN uint64, cfg map[string]any, seeds []PoolCorpusSeed) (uint64, []byte) {
	if ParseInputMode(cfg) == InputModeBytes {
		if len(seeds) == 0 {
			b := DeriveInputBytes(inputN, cfg)
			return PackInputBytesToU64(b), b
		}
		seed := PickWeightedSeedForConfig(seeds, inputN, cfg)
		cap := PowerMutCap(cfg)
		if CorpusExploreV2Enabled(cfg) && cap < 10 {
			cap = 10
		}
		stageCount := StageDeterministicMax + cap
		mutIdx := int(inputN % uint64(MutationsForSeedCapped(seed.Energy, cap)))
		stage := MutationStage((int(inputN) + mutIdx*17 + seed.Energy) % stageCount)
		salt := inputN * 0x9E3779B97F4A7C15
		base := seed.InputBytes
		if len(base) == 0 {
			base = U64LayoutToBytes(seed.Input)
		}
		b := MutateBytesForHunt(base, stage, salt, ParseMaxInputBytes(cfg), cfg, CorpusBytesFromSeeds(seeds))
		return PackInputBytesToU64(b), b
	}
	if len(seeds) == 0 {
		return DeriveInput(inputN, cfg), nil
	}
	seed := PickWeightedSeedForConfig(seeds, inputN, cfg)
	cap := PowerMutCap(cfg)
	if CorpusExploreV2Enabled(cfg) && cap < 10 {
		cap = 10
	}
	stageCount := StageDeterministicMax + cap
	mutIdx := int(inputN % uint64(MutationsForSeedCapped(seed.Energy, cap)))
	stage := MutationStage((int(inputN) + mutIdx*17 + seed.Energy) % stageCount)
	salt := inputN * 0x9E3779B97F4A7C15
	return MutateInput(seed.Input, stage, salt), nil
}

// CorpusObserveBoost returns energy increment after observing a run outcome.
func CorpusObserveBoost(recordFinding bool, newEdge, newPath bool) int {
	if recordFinding {
		return 6
	}
	boost := 1
	if newEdge {
		boost += 2
	}
	if newPath {
		boost++
	}
	return boost
}
