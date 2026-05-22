package gpupoh

import (
	"fmt"
	"os"
	"strings"
)

// cudaComputeArch returns the NVRTC virtual architecture string for a device CC.
// Examples: 12.0 → compute_120, 8.9 → compute_89, 7.5 → compute_75.
func cudaComputeArch(major, minor int) string {
	if major < 0 {
		major = 0
	}
	if minor < 0 {
		minor = 0
	}
	return fmt.Sprintf("compute_%d%d", major, minor)
}

// nvrtcArchChain returns NVRTC --gpu-architecture targets to try (newest first).
func nvrtcArchChain(major, minor int) []string {
	if v := strings.TrimSpace(os.Getenv("HACKME_CUDA_ARCH")); v != "" {
		a := strings.TrimPrefix(strings.TrimSpace(v), "sm_")
		a = strings.TrimPrefix(a, "compute_")
		if strings.HasPrefix(v, "sm_") {
			return []string{"sm_" + a}
		}
		if strings.HasPrefix(v, "compute_") {
			return []string{v}
		}
		return []string{"compute_" + a}
	}
	primary := cudaComputeArch(major, minor)
	fallbacks := []string{
		"compute_120", "compute_100", "compute_90", "compute_89",
		"compute_86", "compute_80", "compute_75", "compute_70", "compute_60",
	}
	out := []string{primary}
	seen := map[string]bool{primary: true}
	for _, f := range fallbacks {
		if !seen[f] {
			out = append(out, f)
			seen[f] = true
		}
	}
	return out
}

// NVRTCArchChainForTest exposes the NVRTC target chain (audit / gputune matrix).
func NVRTCArchChainForTest(major, minor int) []string {
	return nvrtcArchChain(major, minor)
}
