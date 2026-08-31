package fuzzengine

import "strings"

// Depth tier controls campaign depth presets (WASM scan vs native gate vs byte corpus).
type DepthTier string

const (
	DepthWasmOnly       DepthTier = "wasm_only"
	DepthWasmNative     DepthTier = "wasm_native"
	DepthBytesCorpus    DepthTier = "bytes_corpus"
	DepthUpstreamBinary DepthTier = "upstream_binary"
	DepthOSSCVE         DepthTier = "oss_cve"
)

// InputMode selects uint64 vs structured byte inputs for check().
type InputMode string

const (
	InputModeUint64 InputMode = "uint64"
	InputModeBytes  InputMode = "bytes"
)

// DepthPreset is the recommended budget for a customer-facing tier.
type DepthPreset struct {
	Tier                 DepthTier
	InputMode            InputMode
	BudgetHMC            float64
	BudgetRuns           int
	BountyRequiresNative bool
	NativeReproEnabled   bool
	UpstreamTarget       string
}

var depthPresets = map[DepthTier]DepthPreset{
	DepthWasmOnly: {
		Tier: DepthWasmOnly, InputMode: InputModeUint64,
		BudgetHMC: 1.0, BudgetRuns: 64,
		BountyRequiresNative: false, NativeReproEnabled: false,
	},
	DepthWasmNative: {
		Tier: DepthWasmNative, InputMode: InputModeUint64,
		BudgetHMC: 5.0, BudgetRuns: 256,
		BountyRequiresNative: true, NativeReproEnabled: true,
		UpstreamTarget: "bitcoin",
	},
	DepthBytesCorpus: {
		Tier: DepthBytesCorpus, InputMode: InputModeBytes,
		BudgetHMC: 25.0, BudgetRuns: 2048,
		BountyRequiresNative: true, NativeReproEnabled: true,
		UpstreamTarget: "bitcoin",
	},
	DepthUpstreamBinary: {
		Tier: DepthUpstreamBinary, InputMode: InputModeBytes,
		BudgetHMC: 15.0, BudgetRuns: 512,
		BountyRequiresNative: true, NativeReproEnabled: true,
		UpstreamTarget: "bitcoin",
	},
	DepthOSSCVE: {
		Tier: DepthOSSCVE, InputMode: InputModeBytes,
		BudgetHMC: 25.0, BudgetRuns: 2000,
		BountyRequiresNative: true, NativeReproEnabled: true,
		UpstreamTarget: "oss",
	},
}

// ParseDepthTier reads config depth_tier (defaults wasm_only).
func ParseDepthTier(cfg map[string]any) DepthTier {
	if cfg == nil {
		return DepthWasmOnly
	}
	s := strings.TrimSpace(strings.ToLower(toString(cfg["depth_tier"])))
	switch s {
	case string(DepthWasmNative), "wasm+native", "native":
		return DepthWasmNative
	case string(DepthBytesCorpus), "bytes", "tx_corpus":
		return DepthBytesCorpus
	case string(DepthUpstreamBinary), "tier_c", "asan_binary", "upstream":
		return DepthUpstreamBinary
	case string(DepthOSSCVE), "cve_hunt":
		return DepthOSSCVE
	default:
		return DepthWasmOnly
	}
}

// ParseInputMode reads input_mode or infers from depth_tier.
func ParseInputMode(cfg map[string]any) InputMode {
	if cfg == nil {
		return InputModeUint64
	}
	s := strings.TrimSpace(strings.ToLower(toString(cfg["input_mode"])))
	switch s {
	case string(InputModeBytes), "byte", "corpus":
		return InputModeBytes
	case string(InputModeUint64), "u64", "":
		if t := ParseDepthTier(cfg); t == DepthBytesCorpus || t == DepthUpstreamBinary || t == DepthOSSCVE {
			return InputModeBytes
		}
		return InputModeUint64
	default:
		if t := ParseDepthTier(cfg); t == DepthBytesCorpus || t == DepthUpstreamBinary || t == DepthOSSCVE {
			return InputModeBytes
		}
		return InputModeUint64
	}
}

// BountyRequiresNative is true when escrow bounty pays only after native_confirmed.
func BountyRequiresNative(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	if v, ok := cfg["bounty_requires_native"]; ok {
		return strings.EqualFold(strings.TrimSpace(toString(v)), "true") || toString(v) == "1"
	}
	tier := ParseDepthTier(cfg)
	return tier == DepthWasmNative || tier == DepthBytesCorpus || tier == DepthUpstreamBinary || tier == DepthOSSCVE
}

// NativeReproEnabled is true when findings enqueue the native repro bridge.
func NativeReproEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	if v, ok := cfg["native_repro_enabled"]; ok {
		if strings.EqualFold(strings.TrimSpace(toString(v)), "false") || toString(v) == "0" {
			return false
		}
		return true
	}
	tier := ParseDepthTier(cfg)
	return tier == DepthWasmNative || tier == DepthBytesCorpus || tier == DepthUpstreamBinary || tier == DepthOSSCVE
}

// NativeReproMode returns asan_binary for upstream_binary tier unless overridden.
func NativeReproMode(cfg map[string]any) string {
	if cfg == nil {
		return "go_port"
	}
	if s := strings.TrimSpace(toString(cfg["native_repro_mode"])); s != "" {
		return strings.ToLower(s)
	}
	if ParseDepthTier(cfg) == DepthUpstreamBinary {
		return "asan_binary"
	}
	if ParseDepthTier(cfg) == DepthOSSCVE {
		return "oss_upstream"
	}
	return "go_port"
}

// GuidedSchedulingEnabled is true when campaign config opts into lab-style corpus scheduling (post-exchange pilot).
func GuidedSchedulingEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(toString(cfg["guided_scheduling"])), "true") || toString(cfg["guided_scheduling"]) == "1"
}

// UpstreamTarget returns pinned upstream key from pins.json (e.g. bitcoin).
func UpstreamTarget(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	if s := strings.TrimSpace(toString(cfg["upstream_target"])); s != "" {
		return s
	}
	preset, ok := depthPresets[ParseDepthTier(cfg)]
	if !ok || preset.UpstreamTarget == "" {
		return ""
	}
	return preset.UpstreamTarget
}

// ApplyDepthTier merges tier defaults into cfg without overriding explicit customer fields.
func ApplyDepthTier(cfg map[string]any, tier DepthTier) map[string]any {
	if cfg == nil {
		cfg = map[string]any{}
	}
	preset, ok := depthPresets[tier]
	if !ok {
		preset = depthPresets[DepthWasmOnly]
	}
	cfg["depth_tier"] = string(preset.Tier)
	if _, ok := cfg["input_mode"]; !ok {
		cfg["input_mode"] = string(preset.InputMode)
	}
	if _, ok := cfg["bounty_requires_native"]; !ok && preset.BountyRequiresNative {
		cfg["bounty_requires_native"] = true
	}
	if _, ok := cfg["native_repro_enabled"]; !ok && preset.NativeReproEnabled {
		cfg["native_repro_enabled"] = true
	}
	if _, ok := cfg["upstream_target"]; !ok && preset.UpstreamTarget != "" {
		cfg["upstream_target"] = preset.UpstreamTarget
	}
	if preset.InputMode == InputModeBytes {
		if _, ok := cfg["seed_byte_corpus"]; !ok {
			cfg["seed_byte_corpus"] = DefaultByteSeedCorpus()
		}
		if _, ok := cfg["max_input_bytes"]; !ok {
			cfg["max_input_bytes"] = DefaultMaxInputBytesStd
		}
	}
	// Honest signal / config shape per tier (not just larger budgets).
	switch preset.Tier {
	case DepthWasmOnly:
		if _, ok := cfg["signal_types"]; !ok {
			cfg["signal_types"] = []string{"wasm_smoke"}
		}
		if _, ok := cfg["exec_per_unit"]; !ok {
			cfg["exec_per_unit"] = defaultExecPerUnitScan
		}
		if _, ok := cfg["power_mut_cap"]; !ok {
			cfg["power_mut_cap"] = DefaultPowerMutCap(DepthWasmOnly)
		}
	case DepthWasmNative:
		if _, ok := cfg["signal_types"]; !ok {
			cfg["signal_types"] = []string{"wasm_check", "native_repro", "segment_exec"}
		}
		if _, ok := cfg["native_repro_mode"]; !ok {
			cfg["native_repro_mode"] = "go_port"
		}
		if _, ok := cfg["exec_per_unit"]; !ok {
			cfg["exec_per_unit"] = defaultExecPerUnitAudit
		}
		if _, ok := cfg["coverage_kind"]; !ok {
			cfg["coverage_kind"] = "input_fingerprint"
		}
		if _, ok := cfg["guided_scheduling"]; !ok {
			cfg["guided_scheduling"] = true
		}
		if _, ok := cfg["power_mut_cap"]; !ok {
			cfg["power_mut_cap"] = DefaultPowerMutCap(DepthWasmNative)
		}
		if _, ok := cfg["mutation_rounds"]; !ok {
			cfg["mutation_rounds"] = 6
		}
	case DepthBytesCorpus:
		if _, ok := cfg["mutation_rounds"]; !ok {
			cfg["mutation_rounds"] = 12
		}
		if _, ok := cfg["coverage_guided"]; !ok {
			cfg["coverage_guided"] = true
		}
		if _, ok := cfg["coverage_kind"]; !ok {
			cfg["coverage_kind"] = "input_fingerprint"
		}
		if _, ok := cfg["exec_per_unit"]; !ok {
			cfg["exec_per_unit"] = defaultExecPerUnitDeep
		}
		if _, ok := cfg["signal_types"]; !ok {
			cfg["signal_types"] = []string{"byte_corpus", "structured_mutation", "corpus_scheduling", "segment_exec", "native_repro"}
		}
		if _, ok := cfg["corpus_hours_budget"]; !ok {
			cfg["corpus_hours_budget"] = true
		}
		if _, ok := cfg["guided_scheduling"]; !ok {
			cfg["guided_scheduling"] = true
		}
		if _, ok := cfg["power_mut_cap"]; !ok {
			cfg["power_mut_cap"] = DefaultPowerMutCap(DepthBytesCorpus)
		}
	case DepthUpstreamBinary:
		if _, ok := cfg["native_repro_mode"]; !ok {
			cfg["native_repro_mode"] = "asan_binary"
		}
		if _, ok := cfg["signal_types"]; !ok {
			cfg["signal_types"] = []string{"asan_binary", "byte_corpus", "native_repro"}
		}
	case DepthOSSCVE:
		if _, ok := cfg["native_repro_mode"]; !ok {
			cfg["native_repro_mode"] = "oss_upstream"
		}
		if _, ok := cfg["oss_cve_hunt"]; !ok {
			cfg["oss_cve_hunt"] = true
		}
		if _, ok := cfg["signal_types"]; !ok {
			cfg["signal_types"] = []string{"oss_cve_hunt", "byte_corpus", "native_repro"}
		}
	}
	cfg["fuzz_engine_version"] = Version
	return cfg
}

// DepthPresetFor returns the catalog preset for a tier.
func DepthPresetFor(tier DepthTier) (DepthPreset, bool) {
	p, ok := depthPresets[tier]
	return p, ok
}
