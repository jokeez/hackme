package hunt

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Coverage-guided Hunt engines (native ASAN path only — never Dig/WASM).
const (
	EngineByte      = "byte"      // legacy L1 mutator without coverage feedback
	EngineLibFuzzer = "libfuzzer" // fleet default
	EngineAFLPP     = "aflpp"     // Deep / long profile
)

// Engine profiles select default wall + engine preference.
const (
	EngineProfileFleet = "fleet"
	EngineProfileDeep  = "deep"
)

// Default fleet / deep lease walls (seconds). Caps keep reclaim healthy.
const (
	DefaultLibFuzzerLeaseSec = 180
	DefaultAFLPPLeaseSec     = 180
	MaxEngineLeaseSec        = 300
	MinEngineLeaseSec        = 15

	// Pool package walls. Reclaim lease is ≥2× wall (see leaseSecondsForConfig)
	// and clips at 480s, so heavy 300s lands on that cap (wall + 180s slack).
	// Throughput inside the wall (fork + len_control) is what multiplies execs;
	// these seconds are the budget that actually runs.
	PoolEngineLeaseSecLite     = 180
	PoolEngineLeaseSecStandard = 240
	PoolEngineLeaseSecHeavy    = 300
)

// ParseHuntEngine normalizes campaign/claim engine ids.
// Empty / auto → libfuzzer (fleet default). Unknown → byte.
func ParseHuntEngine(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "auto", "default", "coverage", "lf":
		return EngineLibFuzzer
	case EngineLibFuzzer, "lib-fuzzer", "lib_fuzzer":
		return EngineLibFuzzer
	case EngineAFLPP, "afl++", "afl", "aflplusplus":
		return EngineAFLPP
	case EngineByte, "legacy", "mutator":
		return EngineByte
	default:
		return EngineByte
	}
}

// IsCoverageEngine reports libFuzzer or AFL++ (not byte fallback).
func IsCoverageEngine(engine string) bool {
	switch ParseHuntEngine(engine) {
	case EngineLibFuzzer, EngineAFLPP:
		return true
	default:
		return false
	}
}

// HuntEngineFromConfig picks engine for a Hunt campaign.
// Prefer explicit hunt_engine; else deep/heavy → aflpp only when HACKME_HUNT_ENGINE_DEEP_AFL=1
// (default OFF — published catalog harnesses are LF/ASAN; afl-fuzz exits in <1s without afl-cc).
// Inventory / custom published harnesses always prefer libfuzzer. HACKME_HUNT_ENGINE forces override.
func HuntEngineFromConfig(cfg map[string]any) string {
	if v := strings.TrimSpace(os.Getenv("HACKME_HUNT_ENGINE")); v != "" {
		return ParseHuntEngine(v)
	}
	if cfg != nil {
		if v := strings.TrimSpace(engineCfgString(cfg["hunt_engine"])); v != "" && !strings.EqualFold(v, "auto") {
			eng := ParseHuntEngine(v)
			// Inventory cannot AFL-instrument catalog-style — coerce aflpp → libfuzzer.
			if eng == EngineAFLPP && isInventoryHuntCfg(cfg) {
				return EngineLibFuzzer
			}
			return eng
		}
	}
	if isInventoryHuntCfg(cfg) {
		return EngineLibFuzzer
	}
	profile := EngineProfileFromConfig(cfg)
	if profile == EngineProfileDeep && envTruthyDefaultOff("HACKME_HUNT_ENGINE_DEEP_AFL") {
		return EngineAFLPP
	}
	return EngineLibFuzzer
}

// EngineProfileFromConfig returns fleet|deep from depth / package.
func EngineProfileFromConfig(cfg map[string]any) string {
	if cfg == nil {
		return EngineProfileFleet
	}
	if v := strings.TrimSpace(strings.ToLower(engineCfgString(cfg["hunt_engine_profile"]))); v == EngineProfileDeep || v == EngineProfileFleet {
		return v
	}
	// Custom inventory fleet smoke — never deep/AFL by package name alone.
	if isInventoryHuntCfg(cfg) {
		return EngineProfileFleet
	}
	pkg := strings.TrimSpace(strings.ToLower(engineCfgString(cfg["hunt_package"])))
	if pkg == "" {
		pkg = strings.TrimSpace(strings.ToLower(engineCfgString(cfg["package"])))
	}
	switch pkg {
	case "hunt_heavy", "heavy", "hunt_standard", "standard":
		return EngineProfileDeep
	}
	tier := strings.TrimSpace(strings.ToLower(engineCfgString(cfg["depth_tier"])))
	if strings.Contains(tier, "heavy") || strings.Contains(tier, "standard") {
		return EngineProfileDeep
	}
	return EngineProfileFleet
}

func isInventoryHuntCfg(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(engineCfgString(cfg["hunt_source"])), "inventory") {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(engineCfgString(cfg["upstream_target"])), "inventory") {
		return true
	}
	id := strings.TrimSpace(engineCfgString(cfg["upstream_target_id"]))
	return strings.HasPrefix(id, "inv_")
}

// ApplyCoverageEngineDefaults stamps hunt_engine / profile on campaign config.
// Default fleet-wide to libfuzzer (published harnesses are LF/ASAN). Opt into AFL++ via
// explicit hunt_engine=aflpp or HACKME_HUNT_ENGINE_DEEP_AFL=1 on the claim path.
func ApplyCoverageEngineDefaults(cfg map[string]any, pkgKey string) {
	if cfg == nil {
		return
	}
	pkgKey = strings.TrimSpace(strings.ToLower(pkgKey))
	inv := isInventoryHuntCfg(cfg)
	lite := pkgKey == "hunt_lite" || pkgKey == "lite" || pkgKey == ""
	if inv || lite {
		cfg["hunt_engine_profile"] = EngineProfileFleet
		cfg["hunt_engine"] = EngineLibFuzzer
		return
	}
	if _, ok := cfg["hunt_engine_profile"]; !ok {
		cfg["hunt_engine_profile"] = EngineProfileDeep
	}
	if _, ok := cfg["hunt_engine"]; !ok {
		cfg["hunt_engine"] = EngineLibFuzzer
	}
}

// PoolEngineLeaseSecForPackage is the pool_distributed engine wall for a Hunt SKU.
func PoolEngineLeaseSecForPackage(pkgKey string) int {
	switch strings.TrimSpace(strings.ToLower(pkgKey)) {
	case "hunt_heavy", "heavy":
		return PoolEngineLeaseSecHeavy
	case "hunt_standard", "standard":
		return PoolEngineLeaseSecStandard
	default:
		return PoolEngineLeaseSecLite
	}
}

// EngineLeaseSecFromConfig returns wall seconds for a coverage-guided Hunt shard.
func EngineLeaseSecFromConfig(cfg map[string]any, engine string) int {
	engine = ParseHuntEngine(engine)
	if !IsCoverageEngine(engine) {
		return 0
	}
	if cfg != nil {
		if n := intFromAny(cfg["engine_lease_sec"]); n > 0 {
			return clampEngineLeaseSec(n)
		}
		if n := intFromAny(cfg["hunt_engine_lease_sec"]); n > 0 {
			return clampEngineLeaseSec(n)
		}
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_HUNT_ENGINE_LEASE_SEC")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return clampEngineLeaseSec(n)
		}
	}
	if engine == EngineAFLPP || EngineProfileFromConfig(cfg) == EngineProfileDeep {
		return DefaultAFLPPLeaseSec
	}
	return DefaultLibFuzzerLeaseSec
}

func clampEngineLeaseSec(n int) int {
	if n < MinEngineLeaseSec {
		return MinEngineLeaseSec
	}
	if n > MaxEngineLeaseSec {
		return MaxEngineLeaseSec
	}
	return n
}

func envTruthyDefaultOn(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return true
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func envTruthyDefaultOff(key string) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return false
	}
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func intFromAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func engineCfgString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		if v == nil {
			return ""
		}
		return fmt.Sprint(v)
	}
}

// CoverageKindForEngine is the claim coverage_kind for reporting.
func CoverageKindForEngine(engine string) string {
	switch ParseHuntEngine(engine) {
	case EngineLibFuzzer:
		return "hunt_libfuzzer"
	case EngineAFLPP:
		return "hunt_aflpp"
	default:
		return "input_fingerprint"
	}
}
