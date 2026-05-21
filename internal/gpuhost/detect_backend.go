package gpuhost

import (
	"os"
	"path/filepath"
	"strings"
)

// BackendChoiceInput is host inventory plus optional worker binary paths.
type BackendChoiceInput struct {
	Report           HostGPUReport
	RepoRoot         string
	ForceOpenCL      bool
	GPUDisabled      bool
	HasCUDAWorkerBin bool
	HasOCLWorkerBin  bool
	NVIDIASMIOK      bool
}

// ResolveBackend picks cuda | opencl | cpu from host GPUs and available worker binaries.
func ResolveBackend(in BackendChoiceInput) string {
	if in.GPUDisabled {
		return "cpu"
	}
	if in.ForceOpenCL {
		return "opencl"
	}
	rep := in.Report
	if !rep.HasNVIDIA && !rep.HasAMD && !rep.HasIntel && len(rep.Names) == 0 {
		return "cpu"
	}
	if rep.HasNVIDIA {
		if in.HasCUDAWorkerBin && in.NVIDIASMIOK {
			return "cuda"
		}
		if in.HasOCLWorkerBin {
			return "opencl"
		}
		if in.NVIDIASMIOK && in.HasCUDAWorkerBin {
			return "cuda"
		}
	}
	if rep.HasAMD || rep.HasIntel {
		if in.HasOCLWorkerBin {
			return "opencl"
		}
		return "opencl"
	}
	if in.NVIDIASMIOK && in.HasCUDAWorkerBin {
		return "cuda"
	}
	if len(rep.Names) > 0 && in.HasOCLWorkerBin {
		return "opencl"
	}
	if len(rep.Names) > 0 {
		return "opencl"
	}
	return "cpu"
}

// ProbeWorkerBins checks repo bin/ for workerpoh-cuda and workerpoh-opencl.
func ProbeWorkerBins(repoRoot string) (cudaBin, oclBin bool) {
	if repoRoot == "" {
		return false, false
	}
	cudaPath := filepath.Join(repoRoot, "bin", "workerpoh-cuda")
	if st, err := os.Stat(cudaPath); err == nil && !st.IsDir() {
		cudaBin = true
	}
	oclPath := filepath.Join(repoRoot, "bin", "workerpoh-opencl")
	if st, err := os.Stat(oclPath); err == nil && !st.IsDir() {
		oclBin = true
	}
	return cudaBin, oclBin
}

func envTruthy(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}
