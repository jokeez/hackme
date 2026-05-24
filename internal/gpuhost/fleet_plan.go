package gpuhost

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"hackme/internal/gputune"
)

// FleetSlot is one pool worker process (one GPU, one backend).
type FleetSlot struct {
	WorkerSuffix string            `json:"worker_suffix"`
	Backend      string            `json:"backend"` // cuda | opencl
	DeviceIndex  int               `json:"device_index"`
	GPUName      string            `json:"gpu_name,omitempty"`
	RigProfileID string            `json:"rig_profile_id,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
}

// FleetPlan describes how many workers to spawn on this host.
type FleetPlan struct {
	Hybrid      bool        `json:"hybrid"`
	CUDACount   int         `json:"cuda_count"`
	OpenCLCount int         `json:"opencl_count"`
	TotalSlots  int         `json:"total_slots"`
	Slots       []FleetSlot `json:"slots"`
	Notes       []string    `json:"notes,omitempty"`
}

// FleetPlanInput controls hybrid / caps.
type FleetPlanInput struct {
	RepoRoot       string
	WorkerIDBase   string
	HybridMode     string // auto | 1 | 0
	FleetEnabled   bool
	FleetMax       int
	ForceOpenCL    bool
	GPUDisabled    bool
	ExplicitDevice int // >=0 pins single device (disables fleet)
}

func envTruthyFleet(key string) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func hybridModeFromEnv() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_GPU_HYBRID")); v != "" {
		return strings.ToLower(v)
	}
	return "auto"
}

func fleetMaxFromEnv() int {
	if v := strings.TrimSpace(os.Getenv("HACKME_GPU_FLEET_MAX")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 20
}

// BuildFleetPlan returns worker slots for this host (CUDA fleet, OpenCL fleet, or hybrid).
func BuildFleetPlan(in FleetPlanInput) FleetPlan {
	out := FleetPlan{Notes: []string{}}
	if in.WorkerIDBase == "" {
		in.WorkerIDBase = "worker-local"
	}
	if in.FleetMax <= 0 {
		in.FleetMax = 20
	}
	if in.RepoRoot == "" {
		in.RepoRoot = "."
	}
	rep := DetectHostGPUs()
	cudaBin, oclBin := ProbeWorkerBins(in.RepoRoot)
	hybrid := false
	switch strings.ToLower(strings.TrimSpace(in.HybridMode)) {
	case "0", "false", "no", "off":
		hybrid = false
	case "1", "true", "yes", "on":
		hybrid = rep.HasNVIDIA && rep.HasAMD && cudaBin && oclBin && !in.GPUDisabled
	default: // auto
		hybrid = rep.HasNVIDIA && rep.HasAMD && cudaBin && oclBin && !in.GPUDisabled && !in.ForceOpenCL
	}
	out.Hybrid = hybrid

	if in.GPUDisabled || in.ExplicitDevice >= 0 {
		backend := ResolveBackend(BackendChoiceInput{
			Report: rep, RepoRoot: in.RepoRoot, ForceOpenCL: in.ForceOpenCL,
			GPUDisabled: in.GPUDisabled, HasCUDAWorkerBin: cudaBin, HasOCLWorkerBin: oclBin,
			NVIDIASMIOK: NVIDIASMILinesOK(),
		})
		dev := in.ExplicitDevice
		if dev < 0 {
			dev = 0
		}
		name := pickGPUNameForBackend(rep, backend, dev)
		out.Slots = append(out.Slots, slotForGPU(in.WorkerIDBase, "", backend, dev, name))
		out.TotalSlots = len(out.Slots)
		return out
	}

	cudaN, oclSlots := platformGPUInventory(in.ForceOpenCL)
	out.CUDACount = cudaN
	out.OpenCLCount = len(oclSlots)

	if hybrid && in.FleetEnabled {
		for i := 0; i < cudaN && len(out.Slots) < in.FleetMax; i++ {
			name := nvidiaNameAt(i)
			if name == "" {
				name = "NVIDIA GPU"
			}
			out.Slots = append(out.Slots, slotForGPU(in.WorkerIDBase, "-cuda"+strconv.Itoa(i), "cuda", i, name))
		}
		for _, oc := range oclSlots {
			if len(out.Slots) >= in.FleetMax {
				break
			}
			sfx := "-ocl" + strconv.Itoa(oc.Index)
			out.Slots = append(out.Slots, slotForGPU(in.WorkerIDBase, sfx, "opencl", oc.Index, oc.Name))
		}
		out.Notes = append(out.Notes, "Hybrid fleet: one worker per NVIDIA (CUDA) and per AMD/Intel discrete (OpenCL).")
		out.TotalSlots = len(out.Slots)
		return out
	}

	backend := ResolveBackend(BackendChoiceInput{
		Report: rep, RepoRoot: in.RepoRoot, ForceOpenCL: in.ForceOpenCL,
		GPUDisabled: in.GPUDisabled, HasCUDAWorkerBin: cudaBin, HasOCLWorkerBin: oclBin,
		NVIDIASMIOK: NVIDIASMILinesOK(),
	})
	if !in.FleetEnabled || backend == "cpu" {
		name := pickGPUNameForBackend(rep, backend, 0)
		out.Slots = append(out.Slots, slotForGPU(in.WorkerIDBase, "", backend, 0, name))
		out.TotalSlots = 1
		return out
	}

	switch backend {
	case "cuda":
		if cudaN < 1 {
			cudaN = 1
		}
		for i := 0; i < cudaN && len(out.Slots) < in.FleetMax; i++ {
			name := nvidiaNameAt(i)
			sfx := ""
			if cudaN > 1 {
				sfx = "-gpu" + strconv.Itoa(i)
			}
			out.Slots = append(out.Slots, slotForGPU(in.WorkerIDBase, sfx, "cuda", i, name))
		}
	case "opencl":
		if len(oclSlots) == 0 {
			oclSlots = []oclGPU{{Index: 0, Name: "OpenCL GPU"}}
		}
		for _, oc := range oclSlots {
			if len(out.Slots) >= in.FleetMax {
				break
			}
			sfx := ""
			if len(oclSlots) > 1 {
				sfx = "-gpu" + strconv.Itoa(oc.Index)
			}
			out.Slots = append(out.Slots, slotForGPU(in.WorkerIDBase, sfx, "opencl", oc.Index, oc.Name))
		}
	default:
		out.Slots = append(out.Slots, slotForGPU(in.WorkerIDBase, "", "cpu", 0, ""))
	}
	out.TotalSlots = len(out.Slots)
	return out
}

func slotForGPU(base, suffix, backend string, dev int, gpuName string) FleetSlot {
	s := FleetSlot{
		WorkerSuffix: suffix,
		Backend:      backend,
		DeviceIndex:  dev,
		GPUName:      gpuName,
		Env:          map[string]string{},
	}
	if backend == "opencl" {
		s.Env["HACKME_FORCE_OPENCL"] = "1"
		s.Env["HACKME_GPU_BACKEND"] = "opencl"
	} else if backend == "cuda" {
		s.Env["HACKME_GPU_BACKEND"] = "cuda"
	}
	if strings.TrimSpace(gpuName) != "" {
		if p, ok := gputune.DetectRigProfile([]string{gpuName}); ok {
			s.RigProfileID = p.ID
			for k, v := range p.Env {
				if strings.TrimSpace(v) != "" {
					s.Env[k] = v
				}
			}
			// Per-slot backend wins over profile default when hybrid.
			if backend == "cuda" {
				s.Env["HACKME_GPU_BACKEND"] = "cuda"
				delete(s.Env, "HACKME_FORCE_OPENCL")
			}
			if backend == "opencl" {
				s.Env["HACKME_GPU_BACKEND"] = "opencl"
				s.Env["HACKME_FORCE_OPENCL"] = "1"
			}
		}
	}
	return s
}

func pickGPUNameForBackend(rep HostGPUReport, backend string, dev int) string {
	switch backend {
	case "cuda":
		if n := nvidiaNameAt(dev); n != "" {
			return n
		}
	case "opencl":
		_, oclSlots := platformGPUInventory(false)
		for _, oc := range oclSlots {
			if oc.Index == dev {
				return oc.Name
			}
		}
	}
	if len(rep.Names) > 0 {
		if dev >= 0 && dev < len(rep.Names) {
			return rep.Names[dev]
		}
		return rep.Names[0]
	}
	return ""
}

// DefaultFleetPlanFromEnv builds a plan using HACKME_* env (worker_autostart / node).
func DefaultFleetPlanFromEnv(repoRoot, workerBase string) FleetPlan {
	explicit := -1
	if v := strings.TrimSpace(os.Getenv("HACKME_GPU_DEVICE")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			explicit = n
		}
	}
	fleetOn := true
	if v := strings.TrimSpace(os.Getenv("HACKME_GPU_FLEET")); v != "" {
		fleetOn = envTruthyFleet("HACKME_GPU_FLEET")
	}
	return BuildFleetPlan(FleetPlanInput{
		RepoRoot:       repoRoot,
		WorkerIDBase:   workerBase,
		HybridMode:     hybridModeFromEnv(),
		FleetEnabled:   fleetOn,
		FleetMax:       fleetMaxFromEnv(),
		ForceOpenCL:    envTruthyFleet("HACKME_FORCE_OPENCL"),
		GPUDisabled:    envTruthyFleet("HACKME_GPU_DISABLE"),
		ExplicitDevice: explicit,
	})
}

// FindFleetplanBin locates fleetplan next to repo bin/ or PATH.
func FindFleetplanBin(repoRoot string) string {
	if repoRoot != "" {
		for _, name := range []string{"fleetplan", "fleetplan.exe"} {
			p := filepath.Join(repoRoot, "bin", name)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	if p, err := exec.LookPath("fleetplan"); err == nil {
		return p
	}
	return ""
}
