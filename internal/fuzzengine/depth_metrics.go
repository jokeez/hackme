package fuzzengine

import "crypto/sha256"

// MutationDepthStats summarizes uniqueness / depth of a mutation sample set.
type MutationDepthStats struct {
	Samples       int     `json:"samples"`
	UniqueSHA256  int     `json:"unique_sha256"`
	UniqueLens    int     `json:"unique_lengths"`
	AvgLen        float64 `json:"avg_len"`
	MaxLen        int     `json:"max_len"`
	MinLen        int     `json:"min_len"`
	UniqueRatio   float64 `json:"unique_ratio"`
	HavocSamples  int     `json:"havoc_samples"`
	HavocUnique   int     `json:"havoc_unique"`
	HavocUniqueRt float64 `json:"havoc_unique_ratio"`
}

// MeasureMutationDepth runs deterministic mutations and reports uniqueness metrics.
// Useful for A/B engine calibration (not a substitute for ASAN exec/s).
func MeasureMutationDepth(base []byte, dict []byte, corpus [][]byte, samples int, maxLen int) MutationDepthStats {
	if samples < 1 {
		samples = 1
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	if len(base) == 0 {
		base = []byte(`{"a":1}`)
	}
	seen := map[string]struct{}{}
	lens := map[int]struct{}{}
	havocSeen := map[string]struct{}{}
	totalLen := 0
	minL, maxL := int(^uint(0)>>1), 0
	havocN := 0
	for i := 0; i < samples; i++ {
		stage := MutationStage(i % (StageDeterministicMax + 16))
		salt := uint64(i)*0x9E3779B97F4A7C15 + 0xDEAD
		out := mutateBytesWithDict(base, stage, salt, maxLen, dict, corpus)
		sum := sha256.Sum256(out)
		key := string(sum[:])
		seen[key] = struct{}{}
		lens[len(out)] = struct{}{}
		totalLen += len(out)
		if len(out) < minL {
			minL = len(out)
		}
		if len(out) > maxL {
			maxL = len(out)
		}
		if int(stage) >= StageHavocBase {
			havocN++
			havocSeen[key] = struct{}{}
		}
	}
	st := MutationDepthStats{
		Samples:      samples,
		UniqueSHA256: len(seen),
		UniqueLens:   len(lens),
		AvgLen:       float64(totalLen) / float64(samples),
		MaxLen:       maxL,
		MinLen:       minL,
		HavocSamples: havocN,
		HavocUnique:  len(havocSeen),
	}
	if samples > 0 {
		st.UniqueRatio = float64(st.UniqueSHA256) / float64(samples)
	}
	if havocN > 0 {
		st.HavocUniqueRt = float64(st.HavocUnique) / float64(havocN)
	}
	if minL == int(^uint(0)>>1) {
		st.MinLen = 0
	}
	return st
}
