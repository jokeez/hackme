package fuzzengine

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
	return 4
}

// PickWeightedSeed selects a corpus seed deterministically from inputN (anti-cheat stable at claim).
func PickWeightedSeed(seeds []PoolCorpusSeed, inputN uint64) PoolCorpusSeed {
	if len(seeds) == 0 {
		return PoolCorpusSeed{}
	}
	weights := make([]int, len(seeds))
	total := 0
	for i, s := range seeds {
		w := s.Energy * s.Energy
		if w < 1 {
			w = 1
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
		seed := PickWeightedSeed(seeds, inputN)
		cap := PowerMutCap(cfg)
		stageCount := StageDeterministicMax + cap
		mutIdx := int(inputN % uint64(MutationsForSeedCapped(seed.Energy, cap)))
		stage := MutationStage((int(inputN) + mutIdx*17 + seed.Energy) % stageCount)
		salt := inputN * 0x9E3779B97F4A7C15
		base := seed.InputBytes
		if len(base) == 0 {
			base = U64LayoutToBytes(seed.Input)
		}
		b := MutateBytes(base, stage, salt, ParseMaxInputBytes(cfg))
		return PackInputBytesToU64(b), b
	}
	if len(seeds) == 0 {
		return DeriveInput(inputN, cfg), nil
	}
	seed := PickWeightedSeed(seeds, inputN)
	cap := PowerMutCap(cfg)
	stageCount := StageDeterministicMax + cap
	mutIdx := int(inputN % uint64(MutationsForSeedCapped(seed.Energy, cap)))
	stage := MutationStage((int(inputN) + mutIdx*17 + seed.Energy) % stageCount)
	salt := inputN * 0x9E3779B97F4A7C15
	return MutateInput(seed.Input, stage, salt), nil
}

// CorpusObserveBoost returns energy increment after observing a run outcome.
func CorpusObserveBoost(recordFinding bool, newEdge, newPath bool) int {
	if recordFinding {
		return 4
	}
	boost := 1
	if newEdge {
		boost++
	}
	if newPath {
		boost++
	}
	return boost
}
