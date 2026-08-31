package fuzzingcli

import "hackme/internal/fuzzengine"

// ApplyDigEngineToPayload merges tier engine defaults into a wizard/API payload.
func ApplyDigEngineToPayload(payload map[string]any, pkg B2BPackage) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	cfg := fuzzengine.ApplyDepthTier(map[string]any{
		"depth_tier": string(pkg.DepthTier),
	}, pkg.DepthTier)
	if pkg.MutationRounds > 0 {
		cfg["mutation_rounds"] = pkg.MutationRounds
	}
	if pkg.CoverageGuided {
		cfg["coverage_guided"] = true
	}
	if len(pkg.SignalTypes) > 0 {
		payload["signal_types"] = pkg.SignalTypes
	}
	for _, k := range []string{
		"mutation_rounds", "coverage_guided", "power_mut_cap", "guided_scheduling",
		"exec_per_unit", "coverage_kind", "input_mode", "seed_byte_corpus", "max_input_bytes",
	} {
		if v, ok := cfg[k]; ok {
			payload[k] = v
		}
	}
	return payload
}
