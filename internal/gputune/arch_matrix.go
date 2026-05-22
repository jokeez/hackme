package gputune

import (
	"fmt"
	"strings"
)

// GPUCamp is the vendor simulation group (Green / Red / Blue).
type GPUCamp string

const (
	CampGreen GPUCamp = "green" // NVIDIA
	CampRed   GPUCamp = "red"   // AMD
	CampBlue  GPUCamp = "blue"  // Intel
)

// SimArch is a synthetic hardware profile for cross-generation matrix audits.
// Values are conservative heuristics for batch/WASM/OpenCL limits — not live PCI IDs.
type SimArch struct {
	ID                  string  `json:"id"`
	MarketingName       string  `json:"marketing_name"`
	Camp                GPUCamp `json:"camp"`
	Family              string  `json:"family"`
	Backend             string  `json:"backend"` // cuda | opencl
	VRAMMiB             int     `json:"vram_mib"`
	CUDACores           int     `json:"cuda_cores,omitempty"`
	ComputeCapability   string  `json:"compute_capability,omitempty"`
	MinDriverCUDA       string  `json:"min_driver_cuda,omitempty"`
	OpenCLStack         string  `json:"opencl_stack,omitempty"` // rocm | adrenalin | neo
	LocalWorkSize       int     `json:"local_work_size,omitempty"`
	Wavefront           int     `json:"wavefront,omitempty"`
	TargetStableGHS     float64 `json:"target_stable_ghs,omitempty"`
	MaxWorkerBatch      uint64  `json:"max_worker_batch"`
	MaxGPUChunk         uint64  `json:"max_gpu_chunk"`
	SandboxWasmBytes    int     `json:"sandbox_wasm_bytes_hint"`
	LegacyCUDAFallback  bool    `json:"legacy_cuda_fallback"` // Pascal/Turing NVRTC chain
}

// GreenCampCatalog — NVIDIA generations (Pascal → Blackwell/HMAI).
func GreenCampCatalog() []SimArch {
	return []SimArch{
		{ID: "nv_gtx1060", MarketingName: "NVIDIA GeForce GTX 1060 6GB", Camp: CampGreen, Family: "Pascal", Backend: "cuda",
			VRAMMiB: 6144, CUDACores: 1280, ComputeCapability: "6.1", MinDriverCUDA: "11.0", MaxWorkerBatch: 1 << 20, MaxGPUChunk: 1 << 19,
			SandboxWasmBytes: 131072, LegacyCUDAFallback: true},
		{ID: "nv_gtx1080ti", MarketingName: "NVIDIA GeForce GTX 1080 Ti", Camp: CampGreen, Family: "Pascal", Backend: "cuda",
			VRAMMiB: 11264, CUDACores: 3584, ComputeCapability: "6.1", MinDriverCUDA: "11.0", MaxWorkerBatch: 2 << 20, MaxGPUChunk: 1 << 20,
			SandboxWasmBytes: 131072, LegacyCUDAFallback: true},
		{ID: "nv_rtx2060", MarketingName: "NVIDIA GeForce RTX 2060", Camp: CampGreen, Family: "Turing", Backend: "cuda",
			VRAMMiB: 6144, CUDACores: 1920, ComputeCapability: "7.5", MinDriverCUDA: "11.8", MaxWorkerBatch: 2 << 20, MaxGPUChunk: 1 << 20,
			SandboxWasmBytes: 131072, LegacyCUDAFallback: true},
		{ID: "nv_rtx2080s", MarketingName: "NVIDIA GeForce RTX 2080 Super", Camp: CampGreen, Family: "Turing", Backend: "cuda",
			VRAMMiB: 8192, CUDACores: 3072, ComputeCapability: "7.5", MinDriverCUDA: "11.8", MaxWorkerBatch: 4 << 20, MaxGPUChunk: 2 << 20,
			SandboxWasmBytes: 131072, LegacyCUDAFallback: true},
		{ID: "nv_rtx3060", MarketingName: "NVIDIA GeForce RTX 3060", Camp: CampGreen, Family: "Ampere", Backend: "cuda",
			VRAMMiB: 12288, CUDACores: 3584, ComputeCapability: "8.6", MinDriverCUDA: "12.0", MaxWorkerBatch: 4 << 20, MaxGPUChunk: 4 << 20,
			SandboxWasmBytes: 262144},
		{ID: "nv_rtx3080", MarketingName: "NVIDIA GeForce RTX 3080", Camp: CampGreen, Family: "Ampere", Backend: "cuda",
			VRAMMiB: 10240, CUDACores: 8704, ComputeCapability: "8.6", MinDriverCUDA: "12.0", MaxWorkerBatch: 4 << 20, MaxGPUChunk: 4 << 20,
			SandboxWasmBytes: 262144},
		{ID: "nv_rtx4060ti", MarketingName: "NVIDIA GeForce RTX 4060 Ti", Camp: CampGreen, Family: "Ada Lovelace", Backend: "cuda",
			VRAMMiB: 16384, CUDACores: 4352, ComputeCapability: "8.9", MinDriverCUDA: "12.4", MaxWorkerBatch: 4 << 20, MaxGPUChunk: 4 << 20,
			SandboxWasmBytes: 262144},
		{ID: "nv_rtx4090", MarketingName: "NVIDIA GeForce RTX 4090", Camp: CampGreen, Family: "Ada Lovelace", Backend: "cuda",
			VRAMMiB: 24576, CUDACores: 16384, ComputeCapability: "8.9", MinDriverCUDA: "12.4", MaxWorkerBatch: 8 << 20, MaxGPUChunk: 4 << 20,
			SandboxWasmBytes: 524288},
		{ID: "nv_h100", MarketingName: "NVIDIA H100 80GB", Camp: CampGreen, Family: "Hopper", Backend: "cuda",
			VRAMMiB: 81920, CUDACores: 16896, ComputeCapability: "9.0", MinDriverCUDA: "12.6", MaxWorkerBatch: 16 << 20, MaxGPUChunk: 8 << 20,
			SandboxWasmBytes: 1536 * 1024},
		{ID: "nv_b200", MarketingName: "NVIDIA B200", Camp: CampGreen, Family: "Blackwell Datacenter", Backend: "cuda",
			VRAMMiB: 184320, CUDACores: 20000, ComputeCapability: "10.0", MinDriverCUDA: "12.8", MaxWorkerBatch: 16 << 20, MaxGPUChunk: 8 << 20,
			SandboxWasmBytes: 1536 * 1024},
	}
}

// RedCampCatalog — AMD Polaris / RDNA (ROCm / Adrenalin simulation).
func RedCampCatalog() []SimArch {
	return []SimArch{
		{ID: "amd_rx580", MarketingName: "AMD Radeon RX 580", Camp: CampRed, Family: "AMD Polaris/Vega", Backend: "opencl",
			VRAMMiB: 8192, OpenCLStack: "rocm", LocalWorkSize: 256, Wavefront: 64, TargetStableGHS: 3.0,
			MaxWorkerBatch: 2 << 20, MaxGPUChunk: 1 << 19, SandboxWasmBytes: 131072},
		{ID: "amd_rx590", MarketingName: "AMD Radeon RX 590", Camp: CampRed, Family: "AMD Polaris/Vega", Backend: "opencl",
			VRAMMiB: 8192, OpenCLStack: "adrenalin", LocalWorkSize: 256, Wavefront: 64, TargetStableGHS: 3.2,
			MaxWorkerBatch: 2 << 20, MaxGPUChunk: 1 << 19, SandboxWasmBytes: 131072},
		{ID: "amd_rx6700xt", MarketingName: "AMD Radeon RX 6700 XT", Camp: CampRed, Family: "AMD RDNA 2", Backend: "opencl",
			VRAMMiB: 12288, OpenCLStack: "rocm", LocalWorkSize: 256, Wavefront: 32, TargetStableGHS: 25.0,
			MaxWorkerBatch: 4 << 20, MaxGPUChunk: 2 << 20, SandboxWasmBytes: 262144},
		{ID: "amd_rx7900xtx", MarketingName: "AMD Radeon RX 7900 XTX", Camp: CampRed, Family: "AMD RDNA 3", Backend: "opencl",
			VRAMMiB: 24576, OpenCLStack: "rocm", LocalWorkSize: 256, Wavefront: 32, TargetStableGHS: 55.0,
			MaxWorkerBatch: 4 << 20, MaxGPUChunk: 4 << 20, SandboxWasmBytes: 262144},
	}
}

// BlueCampCatalog — Intel Arc OpenCL generic profile.
func BlueCampCatalog() []SimArch {
	return []SimArch{
		{ID: "intel_arc_a770", MarketingName: "Intel Arc A770", Camp: CampBlue, Family: "Intel Arc", Backend: "opencl",
			VRAMMiB: 16384, OpenCLStack: "neo", LocalWorkSize: 256, Wavefront: 16, TargetStableGHS: 12.0,
			MaxWorkerBatch: 2 << 20, MaxGPUChunk: 1 << 20, SandboxWasmBytes: 131072},
	}
}

// AllSimArch returns the full global matrix (Green + Red + Blue).
func AllSimArch() []SimArch {
	out := make([]SimArch, 0, 32)
	out = append(out, GreenCampCatalog()...)
	out = append(out, RedCampCatalog()...)
	out = append(out, BlueCampCatalog()...)
	return out
}

// LookupSimArch finds a synthetic profile by id.
func LookupSimArch(id string) (SimArch, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, a := range AllSimArch() {
		if strings.EqualFold(a.ID, id) {
			return a, true
		}
	}
	return SimArch{}, false
}

// ValidateSimBatch checks worker batch/chunk against simulated VRAM and generation caps.
func ValidateSimBatch(a SimArch, batch, chunk uint64) error {
	if batch == 0 {
		return fmt.Errorf("gputune: batch size must be > 0")
	}
	if batch > a.MaxWorkerBatch {
		return fmt.Errorf("gputune: batch %d exceeds %s max %d", batch, a.ID, a.MaxWorkerBatch)
	}
	if chunk == 0 {
		return fmt.Errorf("gputune: gpu chunk must be > 0")
	}
	if chunk > a.MaxGPUChunk {
		return fmt.Errorf("gputune: chunk %d exceeds %s max %d", chunk, a.ID, a.MaxGPUChunk)
	}
	if chunk > batch {
		return fmt.Errorf("gputune: chunk %d > batch %d", chunk, batch)
	}
	// Rough VRAM guard: batch nonces × 64B scratch heuristic (audit only).
	scratchMiB := int((batch * 64) / (1024 * 1024))
	if scratchMiB > a.VRAMMiB/2 {
		return fmt.Errorf("gputune: batch %d likely exceeds half VRAM on %s (%d MiB)", batch, a.ID, a.VRAMMiB)
	}
	return nil
}

// RecommendSandboxWasmBytes returns WASM check payload cap hint for pool sandbox on this arch class.
func RecommendSandboxWasmBytes(a SimArch) int {
	if a.SandboxWasmBytes > 0 {
		return a.SandboxWasmBytes
	}
	return 131072
}

// SimOpenCLCompile models driver/compiler acceptance (ROCm vs Adrenalin vs Neo).
func SimOpenCLCompile(a SimArch, driverTag string) error {
	tag := strings.ToLower(strings.TrimSpace(driverTag))
	want := strings.ToLower(strings.TrimSpace(a.OpenCLStack))
	if want == "" {
		return nil
	}
	if tag == "" || tag == want || tag == "generic" {
		return nil
	}
	// Cross-stack: allow rocm on linux for adrenalin-named profiles (rusticl path).
	if want == "adrenalin" && (tag == "rocm" || tag == "rusticl") {
		return nil
	}
	return fmt.Errorf("gputune: opencl stack mismatch for %s: want %s got %s", a.ID, want, tag)
}

// SimArchMatchesGPUName links live GPU name strings to the closest matrix entry.
func SimArchMatchesGPUName(a SimArch, gpuName string) bool {
	n := strings.ToLower(strings.TrimSpace(gpuName))
	switch a.ID {
	case "nv_gtx1060":
		return strings.Contains(n, "gtx 1060")
	case "nv_gtx1080ti":
		return strings.Contains(n, "1080 ti")
	case "nv_rtx2060":
		return strings.Contains(n, "rtx 2060")
	case "nv_rtx2080s":
		return strings.Contains(n, "2080 super")
	case "nv_rtx3060":
		return strings.Contains(n, "rtx 3060")
	case "nv_rtx3080":
		return strings.Contains(n, "rtx 3080")
	case "nv_rtx4060ti":
		return strings.Contains(n, "4060 ti")
	case "nv_rtx4090":
		return strings.Contains(n, "rtx 4090")
	case "nv_h100":
		return strings.Contains(n, "h100")
	case "nv_b200":
		return strings.Contains(n, "b200")
	case "amd_rx580":
		return strings.Contains(n, "rx 580")
	case "amd_rx590":
		return strings.Contains(n, "rx 590")
	case "amd_rx6700xt":
		return strings.Contains(n, "6700")
	case "amd_rx7900xtx":
		return strings.Contains(n, "7900")
	case "intel_arc_a770":
		return strings.Contains(n, "arc a770")
	default:
		return strings.Contains(n, strings.ToLower(a.MarketingName))
	}
}
