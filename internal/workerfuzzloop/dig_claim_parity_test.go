package workerfuzzloop

import (
	"encoding/hex"
	"testing"

	"hackme/internal/fuzzengine"
)

// Dig claim mirrors must rebuild the same SegmentExecInput stream as campaign cfg.
func TestDigClaimSegmentParity(t *testing.T) {
	dict := []byte("AKIAASIAghp_github_pat")
	corpus := []any{"41414141", "42424242", "deadbeef"}
	campaign := map[string]any{
		"input_mode":        "bytes",
		"depth_tier":        "bytes_corpus",
		"guided_scheduling": true,
		"exec_per_unit":     8,
		"max_input_bytes":   128,
		"power_mut_cap":     14,
		"corpus_explore_v2": true,
		"mutator_dict":      hex.EncodeToString(dict),
		"seed_byte_corpus":  corpus,
		"coverage_kind":     "input_fingerprint",
	}
	seeds := []fuzzengine.PoolCorpusSeed{
		{Input: 1, InputBytes: []byte("AAAA"), Energy: 2},
		{Input: 2, InputBytes: []byte("BBBB"), Energy: 2},
	}
	cr := ClaimResp{
		InputN:          7,
		InputMode:       "bytes",
		DepthTier:       "bytes_corpus",
		ExecPerUnit:     8,
		MaxInputBytes:   128,
		PowerMutCap:     14,
		CorpusExploreV2: true,
		MutatorDictHex:  hex.EncodeToString(dict),
		SeedByteCorpus:  corpus,
		CoverageKind:    "input_fingerprint",
		CorpusSeeds:     fuzzengine.CorpusSeedsClaimMaps(seeds),
	}
	workerCfg := digWorkerCfgFromClaim(cr)
	for exec := uint64(0); exec < 8; exec++ {
		_, wantB := fuzzengine.SegmentExecInput(7, exec, campaign, seeds)
		_, gotB := fuzzengine.SegmentExecInput(7, exec, workerCfg, seeds)
		if string(wantB) != string(gotB) {
			t.Fatalf("exec=%d dig claim/cfg diverge want=%x got=%x", exec, wantB, gotB)
		}
	}
}

func digWorkerCfgFromClaim(cr ClaimResp) map[string]any {
	cfg := map[string]any{
		"input_mode":    cr.InputMode,
		"depth_tier":    cr.DepthTier,
		"exec_per_unit": cr.ExecPerUnit,
	}
	if cr.MaxInputBytes > 0 {
		cfg["max_input_bytes"] = cr.MaxInputBytes
	}
	if cr.PowerMutCap > 0 {
		cfg["power_mut_cap"] = cr.PowerMutCap
	}
	if cr.CorpusExploreV2 {
		cfg["corpus_explore_v2"] = true
	}
	if len(cr.SeedByteCorpus) > 0 {
		cfg["seed_byte_corpus"] = cr.SeedByteCorpus
	}
	if h := cr.MutatorDictHex; h != "" {
		cfg["mutator_dict"] = h
	}
	if seeds, _ := fuzzengine.CorpusSeedsFromClaimMaps(cr.CorpusSeeds); len(seeds) > 0 {
		cfg["guided_scheduling"] = true
	}
	return cfg
}
