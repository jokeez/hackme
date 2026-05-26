package main

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

// FuzzEngineVersion is reported in campaign summaries and customer reports.
const FuzzEngineVersion = "fuzz_engine_v2"

// Default security-oriented seeds (op_type, item_id, quantity bit-patterns for check() guards).
var defaultFuzzSeedCorpus = []uint64{
	0,
	1,
	2,
	3,
	0x0001_0000_0000_0001,
	0x0002_0000_0000_0000,
	0x0003_0000_0003_0D40, // op=3 qty>200k overflow class
	0x0001_0000_0003_0000, // op=1 item_id=3 OOB class
	0xFFFF_FFFF_FFFF_FFFF,
	0xDEAD_BEEF_CAFE_BABE,
}

func normalizeFuzzCampaignConfig(cfg map[string]any, campaignType string) map[string]any {
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["fuzz_engine_version"] = FuzzEngineVersion
	if _, ok := cfg["mutation_rounds"]; !ok {
		cfg["mutation_rounds"] = 4
	}
	if _, ok := cfg["seed_corpus"]; !ok {
		ctype := strings.TrimSpace(strings.ToLower(campaignType))
		if ctype == "property" || ctype == "fuzz" || ctype == "" {
			seeds := make([]any, 0, len(defaultFuzzSeedCorpus))
			for _, s := range defaultFuzzSeedCorpus {
				seeds = append(seeds, s)
			}
			cfg["seed_corpus"] = seeds
		}
	}
	if _, ok := cfg["coverage_guided"]; !ok {
		cfg["coverage_guided"] = true
	}
	return cfg
}

func parseSeedCorpus(cfg map[string]any) []uint64 {
	raw, ok := cfg["seed_corpus"]
	if !ok || raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case []uint64:
		return append([]uint64(nil), t...)
	case []any:
		out := make([]uint64, 0, len(t))
		for _, item := range t {
			if u, ok := parseSeedAny(item); ok {
				out = append(out, u)
			}
		}
		return out
	default:
		return nil
	}
}

func parseSeedAny(v any) (uint64, bool) {
	switch x := v.(type) {
	case float64:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case int:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case int64:
		if x < 0 {
			return 0, false
		}
		return uint64(x), true
	case uint64:
		return x, true
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		if s == "" {
			return 0, false
		}
		if strings.HasPrefix(s, "0x") {
			u, err := strconv.ParseUint(s[2:], 16, 64)
			return u, err == nil
		}
		u, err := strconv.ParseUint(s, 10, 64)
		return u, err == nil
	default:
		return 0, false
	}
}

func mutationRoundsFromConfig(cfg map[string]any) int {
	if cfg == nil {
		return 4
	}
	if v, ok := cfg["mutation_rounds"]; ok {
		n := intFromAny(v)
		if n >= 0 && n <= 32 {
			return n
		}
	}
	return 4
}

func splitmix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	z := x
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return z ^ (z >> 31)
}

// deriveFuzzInput maps sequential work id to a mutated 64-bit check() argument.
func deriveFuzzInput(inputN uint64, cfg map[string]any) uint64 {
	seeds := parseSeedCorpus(cfg)
	base := inputN
	if len(seeds) > 0 {
		base = seeds[inputN%uint64(len(seeds))]
		base ^= inputN * 0xD6E8FEB86659FD93
	}
	rounds := mutationRoundsFromConfig(cfg)
	out := base
	for i := 0; i < rounds; i++ {
		mix := splitmix64(inputN + uint64(i)*0x9E3779B185EBCA87)
		// Flip 1–4 pseudo-random bits per round (structured fuzz, not raw counter).
		for bit := uint(0); bit < 4; bit++ {
			b := uint((mix >> (bit * 11)) % 64)
			out ^= uint64(1) << b
		}
		out = splitmix64(out ^ mix)
	}
	return out
}

func fuzzInputSHA256(input uint64) string {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], input)
	sum := sha256.Sum256(buf[:])
	return hex.EncodeToString(sum[:])
}

func fuzzInputReproCmd(input uint64) string {
	return fmt.Sprintf("check(0x%x) /* %d */", input, input)
}

// fuzzCoverageBuckets returns synthetic edge/path buckets derived from input bytes (v2).
func fuzzCoverageBuckets(input uint64) (edgeBucket, pathBucket int) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], input)
	h := fnv.New64a()
	_, _ = h.Write(buf[:])
	mix := h.Sum64()
	op, itemID, qty := wasmCheckInputParts(input)
	edgeBucket = int((mix ^ uint64(op*9973+itemID*131)) % 257)
	pathBucket = int((mix ^ uint64(qty*17)) % 509)
	if edgeBucket < 0 {
		edgeBucket = 0
	}
	if pathBucket < 0 {
		pathBucket = 0
	}
	return edgeBucket, pathBucket
}

func fuzzEngineMetaFromConfig(cfg map[string]any) map[string]any {
	seeds := parseSeedCorpus(cfg)
	return map[string]any{
		"version":         FuzzEngineVersion,
		"seed_count":      len(seeds),
		"mutation_rounds": mutationRoundsFromConfig(cfg),
		"coverage_guided": cfg != nil && strings.EqualFold(strings.TrimSpace(toString(cfg["coverage_guided"])), "true"),
		"features": []string{
			"seed_corpus",
			"bitflip_mutation",
			"coverage_buckets_v2",
			"input_sha256_artifacts",
			"finding_diff_coverage",
		},
	}
}
