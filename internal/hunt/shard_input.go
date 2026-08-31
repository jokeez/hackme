package hunt

import (
	"crypto/sha256"
	"fmt"
	"strings"

	"hackme/internal/fuzzengine"
)

// ShardAnchorBytes is the claim-frozen anchor input for one Hunt pool shard (exec 0).
func ShardAnchorBytes(campaignID string, inputN uint64, cfg map[string]any) []byte {
	seed := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%s", campaignID, inputN, cfgString(cfg, "upstream_target_id"))))
	maxB := fuzzengine.ParseMaxInputBytes(cfg)
	if maxB < 16 {
		maxB = 256
	}
	if maxB > 4096 {
		maxB = 4096
	}
	out := make([]byte, maxB)
	copy(out, seed[:])
	if maxB > 32 {
		for i := 32; i < maxB; i++ {
			out[i] = seed[i%32] ^ byte(i)
		}
	}
	return out
}

func huntSegmentSalt(inputN, execIdx uint64) uint64 {
	return inputN ^ (execIdx * 0x9E3779B97F4A7C15)
}

// HuntCorpusGuided reports whether Hunt pool shards use shared corpus scheduling (L2).
func HuntCorpusGuided(cfg map[string]any) bool {
	if cfg != nil {
		if v, ok := cfg["hunt_corpus_guided"]; ok {
			return cfgTruthy(v)
		}
	}
	return fuzzengine.GuidedSchedulingEnabled(cfg)
}

// ApplyPoolGuidedDefaults enables L2 corpus-guided Hunt pool settings.
func ApplyPoolGuidedDefaults(cfg map[string]any, targetID string) {
	if cfg == nil {
		return
	}
	cfg["hunt_corpus_guided"] = true
	cfg["guided_scheduling"] = true
	cfg["coverage_guided"] = true
	cfg["corpus_persist"] = true
	if strings.TrimSpace(targetID) != "" {
		cfg["corpus_persist_key"] = "hunt:" + strings.TrimSpace(targetID)
	}
	if _, ok := cfg["pool_corpus_max"]; !ok {
		cfg["pool_corpus_max"] = 256
	}
}

// ShardSegmentMutating reports whether Hunt pool shards run L1 mutating exec chains.
func ShardSegmentMutating(cfg map[string]any) bool {
	if cfg != nil {
		if v, ok := cfg["hunt_segment_mutating"]; ok {
			return cfgTruthy(v)
		}
	}
	return ShardIterationsPer(cfg) > 1
}

// ShardIterationsPer returns iterations_per_shard for Hunt pool work.
func ShardIterationsPer(cfg map[string]any) int {
	n := int(cfgInt(cfg, "iterations_per_shard"))
	if n < 1 {
		n = defaultHuntIterationsPerShard
	}
	if n > 64 {
		n = 64
	}
	return n
}

// ShardSegmentExecInput derives deterministic input for exec idx within one Hunt shard.
// execIdx 0 is the claim anchor; later execs are deterministic mutations (L1) or corpus-guided (L2).
func ShardSegmentExecInput(campaignID string, inputN, execIdx uint64, cfg map[string]any, seeds []fuzzengine.PoolCorpusSeed) []byte {
	if execIdx == 0 && HuntCorpusGuided(cfg) && len(seeds) > 0 {
		_, b := fuzzengine.GuidedInputForWork(inputN, cfg, seeds)
		return append([]byte(nil), b...)
	}
	if execIdx == 0 || !ShardSegmentMutating(cfg) {
		return append([]byte(nil), ShardAnchorBytes(campaignID, inputN, cfg)...)
	}
	maxLen := fuzzengine.ParseMaxInputBytes(cfg)
	cap := fuzzengine.PowerMutCap(cfg)
	if ShardIterationsPer(cfg) > 1 && cap < 8 {
		cap = 8
	}
	stageCount := fuzzengine.StageDeterministicMax + cap
	stage := fuzzengine.MutationStage(int((inputN + execIdx*131) % uint64(stageCount)))
	salt := huntSegmentSalt(inputN, execIdx)

	var base []byte
	if execIdx > 0 && len(seeds) >= 2 && execIdx%19 == 0 {
		a := fuzzengine.PickWeightedSeed(seeds, inputN)
		b := fuzzengine.PickWeightedSeed(seeds, inputN+execIdx)
		ab, bb := a.InputBytes, b.InputBytes
		if len(ab) == 0 {
			ab = fuzzengine.U64LayoutToBytes(a.Input)
		}
		if len(bb) == 0 {
			bb = fuzzengine.U64LayoutToBytes(b.Input)
		}
		base = spliceShardBytes(ab, bb, maxLen)
	} else {
		base = huntByteAnchorBase(campaignID, inputN, cfg, seeds)
	}
	return fuzzengine.MutateBytesForConfig(base, stage, salt, maxLen, cfg)
}

func huntByteAnchorBase(campaignID string, inputN uint64, cfg map[string]any, seeds []fuzzengine.PoolCorpusSeed) []byte {
	if corpus := fuzzengine.ParseByteCorpus(cfg); len(corpus) > 0 {
		return append([]byte(nil), corpus[inputN%uint64(len(corpus))]...)
	}
	if HuntCorpusGuided(cfg) && len(seeds) > 0 {
		seed := fuzzengine.PickWeightedSeed(seeds, inputN)
		if len(seed.InputBytes) > 0 {
			return append([]byte(nil), seed.InputBytes...)
		}
		return fuzzengine.U64LayoutToBytes(seed.Input)
	}
	return ShardAnchorBytes(campaignID, inputN, cfg)
}

func spliceShardBytes(a, b []byte, maxLen int) []byte {
	if maxLen <= 0 {
		maxLen = fuzzengine.DefaultMaxInputBytesStd
	}
	if len(a) == 0 {
		return append([]byte(nil), b...)
	}
	if len(b) == 0 {
		return append([]byte(nil), a...)
	}
	mid := len(a) / 2
	if mid == 0 {
		mid = 1
	}
	out := append(append([]byte(nil), a[:mid]...), b...)
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}

func cfgString(cfg map[string]any, key string) string {
	if cfg == nil {
		return ""
	}
	v, ok := cfg[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func cfgInt(cfg map[string]any, key string) int {
	if cfg == nil {
		return 0
	}
	v := cfg[key]
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		var n int
		_, _ = fmt.Sscanf(t, "%d", &n)
		return n
	default:
		return 0
	}
}

func cfgTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		switch t {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	case int:
		return t != 0
	case float64:
		return t != 0
	}
	return false
}
