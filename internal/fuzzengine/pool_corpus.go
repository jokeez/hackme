package fuzzengine

import (
	"crypto/sha256"
	"strings"
)

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
	return pickWeightedSeedMode(seeds, inputN, false, nil)
}

// PickWeightedSeedForConfig uses classic weights, or explore_v2 / coverage feedback when enabled.
func PickWeightedSeedForConfig(seeds []PoolCorpusSeed, inputN uint64, cfg map[string]any) PoolCorpusSeed {
	return PickWeightedSeedWithRarity(seeds, inputN, cfg, nil)
}

// PickWeightedSeedWithRarity is the full scheduler: explore weights + optional edge rarity map.
func PickWeightedSeedWithRarity(seeds []PoolCorpusSeed, inputN uint64, cfg map[string]any, rarity EdgeHitCounts) PoolCorpusSeed {
	explore := CorpusExploreV2Enabled(cfg) || CoverageFeedbackEnabled(cfg)
	return pickWeightedSeedMode(seeds, inputN, explore, rarity)
}

func pickWeightedSeedMode(seeds []PoolCorpusSeed, inputN uint64, exploreV2 bool, rarity EdgeHitCounts) PoolCorpusSeed {
	if len(seeds) == 0 {
		return PoolCorpusSeed{}
	}
	weights := make([]int, len(seeds))
	total := 0
	for i, s := range seeds {
		w := SeedScheduleWeight(s, exploreV2, rarity)
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
	return GuidedInputForWorkWithRarity(inputN, cfg, seeds, nil)
}

// GuidedInputForWorkWithRarity applies power schedule using edge rarity when available.
func GuidedInputForWorkWithRarity(inputN uint64, cfg map[string]any, seeds []PoolCorpusSeed, rarity EdgeHitCounts) (uint64, []byte) {
	if rarity == nil && len(seeds) > 0 {
		rarity = BuildEdgeHitCounts(seeds)
	}
	if ParseInputMode(cfg) == InputModeBytes {
		if len(seeds) == 0 {
			b := DeriveInputBytes(inputN, cfg)
			return PackInputBytesToU64(b), b
		}
		seed := PickWeightedSeedWithRarity(seeds, inputN, cfg, rarity)
		cap := PowerMutCap(cfg)
		if (CorpusExploreV2Enabled(cfg) || CoverageFeedbackEnabled(cfg)) && cap < 12 {
			cap = 12
		}
		edgeHits := 0
		if rarity != nil {
			edgeHits = rarity[seed.Edge]
		}
		stage := PowerScheduleStage(inputN, seed.Energy, edgeHits, cap)
		salt := inputN * 0x9E3779B97F4A7C15
		if len(seed.InputBytes) > 0 {
			sum := sha256.Sum256(seed.InputBytes)
			salt ^= uint64(sum[0])<<56 | uint64(sum[1])<<48 | uint64(sum[2])<<40 | uint64(sum[3])<<32
		}
		base := seed.InputBytes
		if len(base) == 0 {
			base = U64LayoutToBytes(seed.Input)
		}
		base = CompactCorpusSeed(base, ParseMaxInputBytes(cfg))
		b := MutateBytesForHunt(base, stage, salt, ParseMaxInputBytes(cfg), cfg, CorpusBytesFromSeeds(seeds))
		return PackInputBytesToU64(b), b
	}
	if len(seeds) == 0 {
		return DeriveInput(inputN, cfg), nil
	}
	seed := PickWeightedSeedWithRarity(seeds, inputN, cfg, rarity)
	cap := PowerMutCap(cfg)
	if (CorpusExploreV2Enabled(cfg) || CoverageFeedbackEnabled(cfg)) && cap < 12 {
		cap = 12
	}
	edgeHits := 0
	if rarity != nil {
		edgeHits = rarity[seed.Edge]
	}
	stage := PowerScheduleStage(inputN, seed.Energy, edgeHits, cap)
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
