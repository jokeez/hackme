// Package fuzzengine implements fuzz_engine_v2 input derivation, coverage buckets,
// and WASM check semantics shared by the node autorunner and pool coordinator workers.
package fuzzengine

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
)

const Version = "fuzz_engine_v2"

// CheckSemantics controls how WASM check(i64)->i32 results map to pass/finding.
//   - pow_gate: pass when check != 0 (mining gate / accepts nonce)
//   - detector: pass when check == 0 (security guard; non-zero = violation)
type CheckSemantics string

const (
	SemanticsPoWGate  CheckSemantics = "pow_gate"
	SemanticsDetector CheckSemantics = "detector"
)

func ParseCheckSemantics(cfg map[string]any) CheckSemantics {
	if cfg == nil {
		return SemanticsPoWGate
	}
	s := strings.TrimSpace(strings.ToLower(toString(cfg["check_semantics"])))
	switch s {
	case "detector", "security", "violation_detector":
		return SemanticsDetector
	default:
		return SemanticsPoWGate
	}
}

// EvalCheck maps raw WASM check() return (i32) to pass and whether to record a finding.
func EvalCheck(sem CheckSemantics, checkRet int32, execErr error) (pass bool, recordFinding bool) {
	if execErr != nil {
		return false, true
	}
	switch sem {
	case SemanticsDetector:
		if checkRet != 0 {
			return false, true
		}
		return true, false
	default:
		if checkRet == 0 {
			return false, true
		}
		return true, false
	}
}

var DefaultSeedCorpus = []uint64{
	0,
	1,
	2,
	3,
	0x0001_0000_0000_0001,
	0x0002_0000_0000_0000,
	0x0003_0000_0003_0D40,
	0x0001_0000_0003_0000,
	0xFFFF_FFFF_FFFF_FFFF,
	0xDEAD_BEEF_CAFE_BABE,
}

func NormalizeCampaignConfig(cfg map[string]any, campaignType string) map[string]any {
	if cfg == nil {
		cfg = map[string]any{}
	}
	cfg["fuzz_engine_version"] = Version
	if _, ok := cfg["mutation_rounds"]; !ok {
		cfg["mutation_rounds"] = 4
	}
	if _, ok := cfg["seed_corpus"]; !ok {
		ctype := strings.TrimSpace(strings.ToLower(campaignType))
		if ctype == "property" || ctype == "fuzz" || ctype == "" {
			seeds := make([]any, 0, len(DefaultSeedCorpus))
			for _, s := range DefaultSeedCorpus {
				seeds = append(seeds, s)
			}
			cfg["seed_corpus"] = seeds
		}
	}
	if _, ok := cfg["coverage_guided"]; !ok {
		cfg["coverage_guided"] = true
	}
	if _, ok := cfg["depth_tier"]; ok {
		cfg = ApplyDepthTier(cfg, ParseDepthTier(cfg))
	}
	if _, ok := cfg["input_mode"]; !ok {
		cfg["input_mode"] = string(ParseInputMode(cfg))
	}
	if _, ok := cfg["check_semantics"]; !ok {
		ctype := strings.TrimSpace(strings.ToLower(campaignType))
		if ctype == "property" || ctype == "fuzz" {
			cfg["check_semantics"] = "detector"
		} else if strings.EqualFold(strings.TrimSpace(toString(cfg["detector_mode"])), "1") ||
			strings.EqualFold(strings.TrimSpace(toString(cfg["detector_mode"])), "true") {
			cfg["check_semantics"] = "detector"
		}
	}
	if _, ok := cfg["coverage_kind"]; !ok {
		cfg["coverage_kind"] = CoverageKind(cfg)
	}
	return cfg
}

func ParseSeedCorpus(cfg map[string]any) []uint64 {
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

func MutationRounds(cfg map[string]any) int {
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

// DeriveInput maps sequential work id to a mutated 64-bit check() argument.
func DeriveInput(inputN uint64, cfg map[string]any) uint64 {
	seeds := ParseSeedCorpus(cfg)
	base := inputN
	if len(seeds) > 0 {
		base = seeds[inputN%uint64(len(seeds))]
		base ^= inputN * 0xD6E8FEB86659FD93
	}
	rounds := MutationRounds(cfg)
	out := base
	for i := 0; i < rounds; i++ {
		mix := splitmix64(inputN + uint64(i)*0x9E3779B185EBCA87)
		for bit := uint(0); bit < 4; bit++ {
			b := uint((mix >> (bit * 11)) % 64)
			out ^= uint64(1) << b
		}
		out = splitmix64(out ^ mix)
	}
	return out
}

func InputSHA256(input uint64) string {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], input)
	sum := sha256.Sum256(buf[:])
	return hex.EncodeToString(sum[:])
}

func ReproCmd(input uint64) string {
	return fmt.Sprintf("check(0x%x) /* %d */", input, input)
}

func WasmCheckInputParts(n uint64) (opType int, itemID int, quantity int64) {
	return int(n & 0xff), int((n >> 8) & 0xffff), int64(n >> 24)
}

// CoverageBuckets returns synthetic edge/path buckets derived from input bytes (v2).
func CoverageBuckets(input uint64) (edgeBucket, pathBucket int) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], input)
	h := fnv.New64a()
	_, _ = h.Write(buf[:])
	mix := h.Sum64()
	op, itemID, qty := WasmCheckInputParts(input)
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

func MetaFromConfig(cfg map[string]any) map[string]any {
	seeds := ParseSeedCorpus(cfg)
	features := []string{
		"seed_corpus",
		"bitflip_mutation",
		"coverage_buckets_v2",
		"input_sha256_artifacts",
		"finding_diff_coverage",
		"segment_exec",
	}
	if ParseCheckSemantics(cfg) == SemanticsDetector {
		features = append(features, "detector_semantics")
	}
	if ParseInputMode(cfg) == InputModeBytes {
		features = append(features, "byte_corpus", "structured_input")
	}
	if NativeReproEnabled(cfg) {
		features = append(features, "native_repro_bridge")
	}
	if NativeReproMode(cfg) == "asan_binary" {
		features = append(features, "asan_binary_repro", "tier_c")
	}
	features = append(features, "stable_crash_buckets")
	if GuidedSchedulingEnabled(cfg) {
		features = append(features, "guided_scheduling")
	}
	if BountyRequiresNative(cfg) {
		features = append(features, "bounty_requires_native")
	}
	return map[string]any{
		"version":           Version,
		"seed_count":        len(seeds),
		"mutation_rounds":   MutationRounds(cfg),
		"coverage_guided":   cfg != nil && strings.EqualFold(strings.TrimSpace(toString(cfg["coverage_guided"])), "true"),
		"coverage_kind":     CoverageKind(cfg),
		"exec_per_unit":     ExecPerUnit(cfg),
		"check_semantics":   string(ParseCheckSemantics(cfg)),
		"depth_tier":        string(ParseDepthTier(cfg)),
		"input_mode":        string(ParseInputMode(cfg)),
		"max_input_bytes":   ParseMaxInputBytes(cfg),
		"guard_pack":        strings.TrimSpace(toString(cfg["guard_pack"])),
		"upstream_target":   UpstreamTarget(cfg),
		"native_repro_mode": NativeReproMode(cfg),
		"features":          features,
	}
}

func ClassifyCheckFail(input uint64, hasWasm bool, sem CheckSemantics) (findingType, severity, title string) {
	if !hasWasm {
		return "property_violation", "medium", fmt.Sprintf("check failed for input %d", input)
	}
	if sem == SemanticsDetector {
		op, itemID, _ := WasmCheckInputParts(input)
		if op == 0x4c {
			return "consensus_script_push", "high", fmt.Sprintf("Script push bound violation (op=0x4c len_field=%d)", itemID)
		}
		return "security_violation", "high", fmt.Sprintf("detector flagged input 0x%x", input)
	}
	op, itemID, qty := WasmCheckInputParts(input)
	switch op {
	case 1:
		if itemID >= 3 {
			return "crash", "critical", fmt.Sprintf("OOB item table read (item_id=%d)", itemID)
		}
	case 2:
		if qty == 0 {
			return "crash", "high", "division by zero in average_spend (quantity=0)"
		}
	case 3:
		if qty > 200000 {
			return "interesting_input", "medium", fmt.Sprintf("integer overflow risk in total_cost (quantity=%d)", qty)
		}
	}
	return "property_violation", "medium", fmt.Sprintf("check returned 0 for input %d", input)
}

// IsHarnessRuntimeTrap reports WASM/runtime failures that are not target-code bugs
// (e.g. closed compiled-module cache races under pool concurrency).
func IsHarnessRuntimeTrap(msg string) bool {
	low := strings.ToLower(strings.TrimSpace(msg))
	if low == "" {
		return false
	}
	for _, needle := range []string{
		"source module must be compiled before instantiation",
		"module has already been closed",
		"closed module",
		"compiled module missing",
		"not a compiled module",
		"deadline exceeded",
		"context deadline exceeded",
		"timed out",
		"timeout",
	} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	return false
}

// ClassifyWasmTrap maps a sandbox/execution error into a finding class.
// Harness/runtime failures are not crash-class (customer gate / top issues).
func ClassifyWasmTrap(input uint64, execErr string, hasWasm bool) (findingType, severity, title string) {
	msg := strings.TrimSpace(execErr)
	titleBase := msg
	if len(titleBase) > 240 {
		titleBase = titleBase[:240]
	}
	low := strings.ToLower(msg)
	if IsHarnessRuntimeTrap(msg) {
		return "harness_runtime", "info", "Harness/runtime: " + titleBase
	}
	if strings.Contains(low, "quarantined") || strings.Contains(low, "trapped during validation") {
		return "sandbox_reject", "info", "Sandbox blocked WASM (invalid or trap-at-load module), not a target-code bug"
	}
	if strings.Contains(low, "divide by zero") {
		return "crash", "high", "WASM trap: integer divide by zero"
	}
	if strings.Contains(low, "out of bounds") || strings.Contains(low, "oob") {
		return "crash", "critical", "WASM trap: out-of-bounds memory access"
	}
	if hasWasm {
		op, itemID, qty := WasmCheckInputParts(input)
		if op == 2 && qty == 0 {
			return "crash", "high", "WASM trap: division by zero in op_type=2"
		}
		if op == 1 && itemID >= 3 {
			return "crash", "critical", fmt.Sprintf("WASM trap during OOB item lookup (item_id=%d)", itemID)
		}
	}
	if strings.Contains(low, "check returned 0") || strings.Contains(low, "property") {
		return "property_violation", "medium", titleBase
	}
	return "crash", "high", "WASM trap: " + titleBase
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}

func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		return ""
	}
}
