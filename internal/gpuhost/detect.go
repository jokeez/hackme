// Package gpuhost — host-side GPU discovery (no CUDA/OpenCL init).
package gpuhost

import "strings"

// HostGPUReport is OS-level GPU inventory for auto backend and rig profiles.
type HostGPUReport struct {
	Names              []string `json:"gpu_names"`
	HasNVIDIA          bool     `json:"has_nvidia"`
	HasAMD             bool     `json:"has_amd"`
	HasIntel           bool     `json:"has_intel"`
	Hybrid             bool     `json:"hybrid"` // NVIDIA + AMD discrete
	SuggestedBackend   string   `json:"suggested_backend"` // cuda | opencl | cpu
	SuggestedProfileID string   `json:"suggested_profile_id,omitempty"`
	VendorSummary      string   `json:"vendor_summary"`
	Notes              []string `json:"notes,omitempty"`
}

// CollectGPUNames returns driver-reported GPU product names on this host.
func CollectGPUNames() []string {
	return detectHostGPUs().Names
}

// DetectHostGPUs probes the OS for discrete/primary GPUs (NVIDIA, AMD, Intel).
func DetectHostGPUs() HostGPUReport {
	return detectHostGPUs()
}

func classifyName(name string) (nvidia, amd, intel bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return false, false, false
	}
	if strings.Contains(n, "nvidia") || strings.Contains(n, "geforce") ||
		strings.Contains(n, "quadro") || strings.Contains(n, "tesla") ||
		strings.Contains(n, "rtx ") || strings.Contains(n, "gtx ") {
		nvidia = true
	}
	if strings.Contains(n, "amd") || strings.Contains(n, "radeon") ||
		strings.Contains(n, "rx ") || strings.Contains(n, "vega") {
		amd = true
	}
	if strings.Contains(n, "intel") && (strings.Contains(n, "arc") || strings.Contains(n, "iris") || strings.Contains(n, "uhd")) {
		intel = true
	}
	if strings.Contains(n, "arc a") || strings.Contains(n, "intel arc") {
		intel = true
	}
	return nvidia, amd, intel
}

func mergeReportNames(rep *HostGPUReport, names ...string) {
	seen := make(map[string]bool, len(rep.Names))
	for _, x := range rep.Names {
		seen[strings.ToLower(strings.TrimSpace(x))] = true
	}
	for _, raw := range names {
		n := strings.TrimSpace(raw)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if seen[key] {
			continue
		}
		seen[key] = true
		rep.Names = append(rep.Names, n)
		nv, amd, intel := classifyName(n)
		rep.HasNVIDIA = rep.HasNVIDIA || nv
		rep.HasAMD = rep.HasAMD || amd
		rep.HasIntel = rep.HasIntel || intel
	}
}

func finalizeHostReport(rep HostGPUReport) HostGPUReport {
	rep.Hybrid = rep.HasNVIDIA && rep.HasAMD
	switch {
	case rep.HasNVIDIA && !rep.HasAMD && !rep.HasIntel:
		rep.VendorSummary = "NVIDIA"
	case rep.HasAMD && !rep.HasNVIDIA && !rep.HasIntel:
		rep.VendorSummary = "AMD"
	case rep.HasIntel && !rep.HasNVIDIA && !rep.HasAMD:
		rep.VendorSummary = "Intel"
	case rep.Hybrid:
		rep.VendorSummary = "NVIDIA+AMD"
	case rep.HasNVIDIA:
		rep.VendorSummary = "NVIDIA (+other)"
	default:
		if len(rep.Names) > 0 {
			rep.VendorSummary = "GPU"
		} else {
			rep.VendorSummary = "none"
		}
	}
	if rep.Hybrid {
		rep.Notes = append(rep.Notes,
			"Hybrid rig: primary worker uses one backend (CUDA for NVIDIA). "+
				"For AMD second GPU set HACKME_GPU_FLEET=1 or a second worker with HACKME_FORCE_OPENCL=1 and HACKME_GPU_DEVICE.")
	}
	return rep
}
