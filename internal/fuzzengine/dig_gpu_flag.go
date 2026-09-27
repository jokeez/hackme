package fuzzengine

import "strings"

// DigGPUMutatorsEnabled reports Dig issue #13 E opt-in (accelerator mutants → CPU eval).
// Mirrors gpudig.Enabled without importing gpudig (avoids fuzzengine↔gpudig cycle).
// SegmentExecInput uses MutateBytesForHunt when this is true — the same stack
// gpudig.GenerateMutants wraps — so worker and coordinator stay byte-identical.
func DigGPUMutatorsEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	v, ok := cfg["dig_gpu_mutators"]
	if !ok {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	default:
		return false
	}
}
