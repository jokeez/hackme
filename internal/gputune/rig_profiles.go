package gputune

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// RigManualOC is vendor-side tuning guidance (not applied by HackMe automatically).
type RigManualOC struct {
	Vendor     string   `json:"vendor"`
	Tools      []string `json:"tools"`
	CoreMHz    string   `json:"core_mhz,omitempty"`
	MemMHz     string   `json:"mem_mhz,omitempty"`
	PowerLimit string   `json:"power_limit,omitempty"`
	Voltage    string   `json:"voltage,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

// RigProfile is a curated env + OC guide for a GPU class (pool worker tuning).
type RigProfile struct {
	ID          string            `json:"id"`
	Label       string            `json:"label"`
	Description string            `json:"description"`
	GPUMatch    []string          `json:"gpu_match"` // substrings on lowercased GPU name
	Env         map[string]string `json:"env"`
	ManualOC    RigManualOC       `json:"manual_oc"`
}

// Rig profiles: stable defaults for Useful PoW pool workers.
var rigProfiles = []RigProfile{
	{
		ID:          "amd_rx580_2048sp",
		Label:       "AMD RX 580 2048SP (Polaris · daily)",
		Description: "Chinese 2048SP refresh / RX 570-class die: conservative batch, OpenCL, thermal guard. Pair with Adrenalin undervolt.",
		GPUMatch:    []string{"rx 580", "2048"},
		Env: map[string]string{
			"HACKME_RIG_PROFILE":              "amd_rx580_2048sp",
			"HACKME_GPU_BACKEND":              "opencl",
			"HACKME_WORKER_BATCH_SIZE":        "1048576",
			"GPU_CHUNK":                       "524288",
			"SEARCH_TIMEOUT_MS":               "4500",
			"HACKME_WORKER_CLAIM_COOLDOWN_MS": "150",
			"HACKME_GPU_TEMP_PAUSE_C":         "78",
			"HACKME_GPU_TEMP_RESUME_C":        "72",
			"HACKME_DESKTOP_GPU_POOL":         "1",
			"HACKME_CUDA_CALIBRATE_GHS":       "3.5",
		},
		ManualOC: RigManualOC{
			Vendor:     "AMD",
			Tools:      []string{"AMD Adrenalin → Performance → Tuning", "MSI Afterburner (optional)"},
			CoreMHz:    "1150–1200 MHz",
			MemMHz:     "2000–2100 MHz effective (start low on 2048SP BIOS)",
			PowerLimit: "−5…−8% power cap",
			Voltage:    "Advanced → undervolt in small steps",
			Notes: []string{
				"2048SP often has tighter memory — raise mem clock only after 30+ min stability in FurMark/OCCT.",
				"Target: <80°C hotspot, no compute artifacts in worker log.",
				"Windows: use workerpoh-opencl.exe (release bundle) for full RX 580 GH/s; CPU-only ~0.02–0.15 GH/s.",
			},
		},
	},
	{
		ID:          "amd_rx580_2048sp_turbo",
		Label:       "AMD RX 580 2048SP (turbo · manual OC)",
		Description: "After stable Adrenalin OC: slightly larger batches. Use only if temps stay under guard.",
		GPUMatch:    []string{"rx 580", "2048"},
		Env: map[string]string{
			"HACKME_RIG_PROFILE":              "amd_rx580_2048sp_turbo",
			"HACKME_GPU_BACKEND":              "opencl",
			"HACKME_WORKER_BATCH_SIZE":        "2097152",
			"GPU_CHUNK":                       "1048576",
			"SEARCH_TIMEOUT_MS":               "5000",
			"HACKME_WORKER_CLAIM_COOLDOWN_MS": "120",
			"HACKME_GPU_TEMP_PAUSE_C":         "80",
			"HACKME_GPU_TEMP_RESUME_C":        "74",
			"HACKME_DESKTOP_GPU_POOL":         "1",
			"HACKME_CUDA_CALIBRATE_GHS":       "0.18",
		},
		ManualOC: RigManualOC{
			Vendor:     "AMD",
			Tools:      []string{"AMD Adrenalin", "MSI Afterburner"},
			CoreMHz:    "1200–1250 MHz (if stable)",
			MemMHz:     "2100–2150 MHz effective",
			PowerLimit: "−3…−5% or stock if cool",
			Notes: []string{
				"Switch from daily profile only after 24h error-free mining.",
				"If coordinator shows 429/rate — raise CLAIM_COOLDOWN_MS to 180.",
			},
		},
	},
	{
		ID:          "amd_rx580_generic",
		Label:       "AMD RX 580 (generic)",
		Description: "Standard RX 580 8GB — balanced pool worker tuning.",
		GPUMatch:    []string{"rx 580"},
		Env: map[string]string{
			"HACKME_RIG_PROFILE":              "amd_rx580_generic",
			"HACKME_GPU_BACKEND":              "opencl",
			"HACKME_WORKER_BATCH_SIZE":        "2097152",
			"GPU_CHUNK":                       "1048576",
			"SEARCH_TIMEOUT_MS":               "4000",
			"HACKME_WORKER_CLAIM_COOLDOWN_MS": "100",
			"HACKME_GPU_TEMP_PAUSE_C":         "80",
			"HACKME_GPU_TEMP_RESUME_C":        "74",
			"HACKME_DESKTOP_GPU_POOL":         "1",
			"HACKME_CUDA_CALIBRATE_GHS":       "0.2",
		},
		ManualOC: RigManualOC{
			Vendor:     "AMD",
			Tools:      []string{"AMD Adrenalin"},
			CoreMHz:    "1150–1250 MHz",
			MemMHz:     "2100–2200 MHz effective",
			PowerLimit: "−5…−10%",
		},
	},
	{
		ID:          "nvidia_rtx_30_daily",
		Label:       "NVIDIA RTX 30xx (daily)",
		Description: "Ampere pool desktop: CUDA, large batch, smart claim cooldown.",
		GPUMatch:    []string{"rtx 30", "rtx 3090", "rtx 3080", "rtx 3070", "rtx 3060"},
		Env: map[string]string{
			"HACKME_RIG_PROFILE":              "nvidia_rtx_30_daily",
			"HACKME_GPU_BACKEND":              "cuda",
			"HACKME_WORKER_BATCH_SIZE":        "4194304",
			"GPU_CHUNK":                       "4194304",
			"SEARCH_TIMEOUT_MS":               "2500",
			"HACKME_WORKER_CLAIM_COOLDOWN_MS": "0",
			"HACKME_GPU_TEMP_PAUSE_C":         "83",
			"HACKME_GPU_TEMP_RESUME_C":        "76",
			"HACKME_DESKTOP_GPU_POOL":         "1",
		},
		ManualOC: RigManualOC{
			Vendor:     "NVIDIA",
			Tools:      []string{"nvidia-smi -pl", "NVIDIA App"},
			PowerLimit: "−10…−15% PL via dashboard Hardware tune",
		},
	},
}

// ListRigProfiles returns all built-in profiles.
func ListRigProfiles() []RigProfile {
	out := make([]RigProfile, len(rigProfiles))
	copy(out, rigProfiles)
	return out
}

// GetRigProfile returns a profile by id.
func GetRigProfile(id string) (RigProfile, bool) {
	id = strings.TrimSpace(strings.ToLower(id))
	for _, p := range rigProfiles {
		if strings.EqualFold(p.ID, id) {
			return p, true
		}
	}
	return RigProfile{}, false
}

// DetectRigProfile picks the best profile for reported GPU names (first match wins).
func DetectRigProfile(gpuNames []string) (RigProfile, bool) {
	if len(gpuNames) == 0 {
		return RigProfile{}, false
	}
	combined := strings.ToLower(strings.Join(gpuNames, " "))
	// Prefer specific 2048SP before generic 580.
	order := []string{"amd_rx580_2048sp", "amd_rx580_generic", "nvidia_rtx_30_daily"}
	for _, pid := range order {
		p, ok := GetRigProfile(pid)
		if !ok {
			continue
		}
		if profileMatchesName(p, combined) {
			return adaptRigProfileForHost(p), true
		}
	}
	for _, p := range rigProfiles {
		if profileMatchesName(p, combined) {
			return adaptRigProfileForHost(p), true
		}
	}
	return RigProfile{}, false
}

func profileMatchesName(p RigProfile, combinedLower string) bool {
	if len(p.GPUMatch) == 0 {
		return false
	}
	for _, needle := range p.GPUMatch {
		if !strings.Contains(combinedLower, strings.ToLower(strings.TrimSpace(needle))) {
			return false
		}
	}
	return true
}

// AdaptRigProfileForHost adjusts env for platform limits (e.g. Windows without OpenCL binary).
func AdaptRigProfileForHost(p RigProfile) RigProfile {
	return adaptRigProfileForHost(p)
}

// adaptRigProfileForHost adjusts env for platform limits (e.g. Windows without OpenCL binary).
func windowsHasOpenCLWorkerExe() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	if sym, err := filepath.EvalSymlinks(exe); err == nil && sym != "" {
		exe = sym
	}
	wp := filepath.Join(filepath.Dir(exe), "workerpoh-opencl.exe")
	st, err := os.Stat(wp)
	return err == nil && !st.IsDir()
}

func adaptRigProfileForHost(p RigProfile) RigProfile {
	if runtime.GOOS != "windows" {
		return p
	}
	out := p
	env := make(map[string]string, len(p.Env))
	for k, v := range p.Env {
		env[k] = v
	}
	// OpenCL binary may be absent in older Windows zips — keep opencl when workerpoh-opencl.exe is shipped.
	if strings.EqualFold(env["HACKME_GPU_BACKEND"], "opencl") && !windowsHasOpenCLWorkerExe() {
		env["HACKME_GPU_BACKEND"] = "auto"
		if _, ok := env["HACKME_WORKER_BATCH_SIZE"]; ok {
			env["HACKME_WORKER_BATCH_SIZE"] = "1048576"
		}
	}
	out.Env = env
	return out
}

// RigProfileEnvKeys are merged into hackme.env (mining tune only).
var RigProfileEnvKeys = []string{
	"HACKME_RIG_PROFILE",
	"HACKME_GPU_BACKEND",
	"HACKME_WORKER_BATCH_SIZE",
	"GPU_CHUNK",
	"SEARCH_TIMEOUT_MS",
	"HACKME_WORKER_CLAIM_COOLDOWN_MS",
	"HACKME_GPU_TEMP_PAUSE_C",
	"HACKME_GPU_TEMP_RESUME_C",
	"HACKME_DESKTOP_GPU_POOL",
	"HACKME_GPU_CALIBRATE_MOD",
	"HACKME_CUDA_CALIBRATE_MOD",
	"HACKME_CUDA_CALIBRATE_GHS",
}
