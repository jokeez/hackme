package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"hackme/internal/gpuhost"
	"hackme/internal/gputune"
)

func hostGPUInventory() gpuhost.HostGPUReport {
	return gpuhost.DetectHostGPUs()
}

func nvidiaSMILinesOK() bool {
	out, err := exec.Command("nvidia-smi", "-L").Output()
	return err == nil && len(out) > 0
}

// resolveAutoGPUBackend picks cuda vs opencl vs cpu from host hardware (Stage 1 — no manual vendor flag).
func resolveAutoGPUBackend(repoRoot string) string {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("HACKME_GPU_DISABLE")), "1") {
		return "cpu"
	}
	if b := strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND")); b != "" && !strings.EqualFold(b, "auto") {
		return b
	}
	rep := hostGPUInventory()
	cudaBin, oclBin := gpuhost.ProbeWorkerBins(repoRoot)
	if runtime.GOOS == "windows" {
		if !cudaBin && !oclBin {
			if wp, err := resolveWorkerpohExePathForBackend(""); err == nil {
				base := strings.ToLower(filepath.Base(wp))
				oclBin = strings.Contains(base, "opencl")
				cudaBin = strings.Contains(base, "cuda")
			}
		}
	}
	backend := gpuhost.ResolveBackend(gpuhost.BackendChoiceInput{
		Report:           rep,
		RepoRoot:         repoRoot,
		ForceOpenCL:      envTruthyGPU("HACKME_FORCE_OPENCL"),
		GPUDisabled:      false,
		HasCUDAWorkerBin: cudaBin,
		HasOCLWorkerBin:  oclBin,
		NVIDIASMIOK:      nvidiaSMILinesOK() || len(queryNVIDIAMulti()) > 0 || len(gpuhost.ListNVIDIAProcCards()) > 0,
	})
	if backend == "cpu" && len(rep.Names) > 0 && oclBin {
		return "opencl"
	}
	return backend
}

func envTruthyGPU(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func enrichHostReportWithProfile(rep gpuhost.HostGPUReport) gpuhost.HostGPUReport {
	if p, ok := gputune.DetectRigProfile(rep.Names); ok {
		rep.SuggestedProfileID = p.ID
		if b := strings.TrimSpace(p.Env["HACKME_GPU_BACKEND"]); b != "" && !strings.EqualFold(b, "auto") {
			// Profile env is a hint; ResolveBackend already ran for worker start.
			if rep.SuggestedBackend == "" || rep.SuggestedBackend == "cpu" {
				rep.SuggestedBackend = b
			}
		}
	}
	if rep.SuggestedBackend == "" {
		rep.SuggestedBackend = resolveAutoGPUBackend(resolveWorkerRepoRoot(""))
	}
	return rep
}
