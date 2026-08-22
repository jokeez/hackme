package poolfuzz

import (
	"encoding/hex"

	"hackme/internal/fuzzengine"
)

const PilotScriptPushCampaignID = "pool-pilot-script-push-guided"

// PilotScriptPushGuidedConfig is the opt-in P3 pilot on script_push_bounds detector WASM.
func PilotScriptPushGuidedConfig(wasmHex string) map[string]any {
	violation := uint64(0x4c | (521 << 8))
	return fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":     true,
		"check_semantics":      "detector",
		"wasm_check_hex":       wasmHex,
		"guided_scheduling":    true,
		"pool_corpus_max":      256,
		"power_mut_cap":        4,
		"seed_corpus":          []any{0, 1, violation},
		"mutation_rounds":      4,
		"stable_crash_buckets": true,
	}, "property")
}

// PilotScriptPushLinearConfig is the control arm for local A/B (same seeds, linear derive).
func PilotScriptPushLinearConfig(wasmHex string) map[string]any {
	cfg := PilotScriptPushGuidedConfig(wasmHex)
	delete(cfg, "guided_scheduling")
	return cfg
}

// PilotScriptPushBytesGuidedConfig is P4 byte-mode pilot (8-byte script layout seeds).
func PilotScriptPushBytesGuidedConfig(wasmHex string) map[string]any {
	return PilotBytesCorpusConfig(wasmHex, fuzzengine.DefaultMaxInputBytesStd, nil, true)
}

// PilotBytesCorpusConfig is the generalized byte corpus pilot (P4 / calibration).
func PilotBytesCorpusConfig(wasmHex string, maxInputBytes int, seedByteCorpus []any, guided bool) map[string]any {
	violation := fuzzengine.U64LayoutToBytes(0x4c | (521 << 8))
	if seedByteCorpus == nil {
		seedByteCorpus = []any{
			"0000000000000000",
			"0100000000000001",
			hex.EncodeToString(violation),
		}
	}
	cfg := map[string]any{
		"pool_distributed":     true,
		"check_semantics":      "detector",
		"wasm_check_hex":       wasmHex,
		"input_mode":           "bytes",
		"max_input_bytes":      maxInputBytes,
		"pool_corpus_max":      256,
		"power_mut_cap":        4,
		"mutation_rounds":      6,
		"stable_crash_buckets": true,
		"seed_byte_corpus":     seedByteCorpus,
	}
	if guided {
		cfg["guided_scheduling"] = true
	}
	return fuzzengine.NormalizeCampaignConfig(cfg, "property")
}

// TracefuseByteSeeds are demo-vulnerable patterns for calibration (full lines).
func TracefuseByteSeeds() []any {
	return []any{
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"GITHUB_PAT=ghp_FAKEEXAMPLETOKENX1234567890123456789",
		"FROM node:latest",
		"ENV API_SECRET=FAKE_EXAMPLE_DOCKER_SECRET_DO_NOT_USE",
		"RUN curl -fsSL https://example.invalid/install-FAKE.sh | sh",
	}
}
