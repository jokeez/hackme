package poolfuzz

import (
	"strings"

	"hackme/internal/fuzzengine"
)

const ProdBytesPilotFlag = "bytes_v1"

// ProdOptInBytesPilotConfig is the post-exchange customer bytes pilot preset.
// Opt-in only: set pilot=bytes_v1 in campaign config; do not enable fleet-wide by default.
func ProdOptInBytesPilotConfig(wasmHex string, maxInputBytes int, seedByteCorpus []any) map[string]any {
	if maxInputBytes <= 0 {
		maxInputBytes = fuzzengine.DefaultMaxInputBytesStd
	}
	cfg := PilotBytesCorpusConfig(wasmHex, maxInputBytes, seedByteCorpus, true)
	cfg["queue_depth"] = 128
	cfg["pilot"] = ProdBytesPilotFlag
	cfg["stable_crash_buckets"] = true
	return cfg
}

// ProdOptInScriptPushBytesPilot uses the script_push guard on bytes tier (internal gate).
func ProdOptInScriptPushBytesPilot(wasmHex string) map[string]any {
	return ProdOptInBytesPilotConfig(wasmHex, fuzzengine.DefaultMaxInputBytesStd, nil)
}

// ProdOptInTracefuseBytesPilot uses Tracefuse-style seeds on 4K tier.
func ProdOptInTracefuseBytesPilot(wasmHex string) map[string]any {
	return ProdOptInBytesPilotConfig(wasmHex, fuzzengine.ByteTierPreset("4k"), TracefuseByteSeeds())
}

// IsBytesPilotCampaign reports whether a campaign is an explicit bytes pilot.
func IsBytesPilotCampaign(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	return fuzzengine.ParseInputMode(cfg) == fuzzengine.InputModeBytes &&
		strings.TrimSpace(toPilotString(cfg["pilot"])) == ProdBytesPilotFlag
}

func toPilotString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return ""
	}
}
