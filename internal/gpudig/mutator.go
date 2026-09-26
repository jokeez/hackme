// Package gpudig: Dig mutant generation on accelerator → CPU WASM/ASAN eval.
// Explicitly NOT GPU ASAN. Issue #13 E — useful PoW honesty.
package gpudig

import (
	"hackme/internal/fuzzengine"
)

// Backend names for cfg dig_gpu_mutator_backend.
const (
	BackendCPU    = "cpu"
	BackendOpenCL = "opencl" // falls back to CPU until a real kernel ships
	BackendCUDA   = "cuda"   // falls back to CPU until a real kernel ships
)

// GenerateMutants produces n deterministic mutants from base.
// GPU backends currently delegate to CPU MutateBytesForHunt — same honesty rule as Hunt:
// accelerator may propose bytes; sanitizer/WASM always runs on CPU.
func GenerateMutants(base []byte, n int, salt uint64, maxLen int, cfg map[string]any, corpus [][]byte) [][]byte {
	if n <= 0 {
		return nil
	}
	if maxLen <= 0 {
		maxLen = fuzzengine.DefaultMaxInputBytesStd
	}
	backend := BackendCPU
	if cfg != nil {
		if v, ok := cfg["dig_gpu_mutator_backend"].(string); ok && v != "" {
			backend = v
		}
	}
	_ = backend // reserved for future OpenCL/CUDA kernels
	out := make([][]byte, 0, n)
	for i := 0; i < n; i++ {
		stage := fuzzengine.MutationStage(fuzzengine.StageHavocBase + (i % 32))
		m := fuzzengine.MutateBytesForHunt(base, stage, salt^uint64(i)*0x9E3779B97F4A7C15, maxLen, cfg, corpus)
		out = append(out, m)
	}
	return out
}

// Enabled reports whether Dig should solicit accelerator mutants (CPU fallback OK).
func Enabled(cfg map[string]any) bool {
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
		s := t
		return s == "1" || s == "true" || s == "yes" || s == "on"
	default:
		return false
	}
}
