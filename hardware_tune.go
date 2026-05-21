package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"math"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"hackme/internal/chain"
	"hackme/internal/gpuhost"
	"hackme/internal/gputune"
)

type gpuTuneDevice struct {
	Index              int                 `json:"index"`
	Name               string              `json:"name"`
	TempC              float64             `json:"temp_c"`
	UtilPct            float64             `json:"util_pct"`
	PowerDrawW         float64             `json:"power_draw_w"`
	PowerLimitW        float64             `json:"power_limit_w"`
	PowerMinW          float64             `json:"power_min_w"`
	PowerMaxW          float64             `json:"power_max_w"`
	SafeMinW           float64             `json:"safe_min_w,omitempty"`
	SafeMaxW           float64             `json:"safe_max_w,omitempty"`
	TurboMaxW          float64             `json:"turbo_max_w,omitempty"`
	PresetEcoW         float64             `json:"preset_eco_w,omitempty"`
	PresetDailyW       float64             `json:"preset_daily_w,omitempty"`
	PresetTurboW       float64             `json:"preset_turbo_w,omitempty"`
	Hints              gputune.Hints       `json:"hints"`
	ManualOC           gputune.RigManualOC `json:"manual_oc,omitempty"`
	PowerReadable      bool                `json:"power_readable"`
	PresetsAvailable   bool                `json:"presets_available"`
	DriverMismatch     bool                `json:"driver_mismatch,omitempty"`
	NvidiaSMIPlCommand string              `json:"nvidia_smi_pl_command,omitempty"`
}

type hardwareTuneEnv struct {
	GPUTempPauseC  float64 `json:"gpu_temp_pause_c"`
	GPUTempResumeC float64 `json:"gpu_temp_resume_c"`
	Note           string  `json:"note"`
}

// cpuTuneBlock is soft CPU throttle info for PoH workers (not OS power plan).
type cpuTuneBlock struct {
	SoftCapPct   float64  `json:"soft_cap_pct"`
	DefaultPct   float64  `json:"default_soft_cap_pct"`
	MinPct       float64  `json:"min_pct"`
	MaxPct       float64  `json:"max_pct"`
	Tips         []string `json:"tips"`
	Note         string   `json:"note"`
	EnvVar       string   `json:"env_var"`
	EnvOverrides string   `json:"env_overrides_note"`
}

type hardwareTuneResponse struct {
	NvidiaSMI          bool            `json:"nvidia_smi"`
	AMDTelemetry       bool            `json:"amd_telemetry"`
	CanSetPowerLimit   bool            `json:"can_set_power_limit"`
	PresetsAvailable   bool            `json:"presets_available"`
	DriverMismatch     bool            `json:"driver_mismatch,omitempty"`
	ActiveRigProfileID string          `json:"active_rig_profile_id,omitempty"`
	PowerLimitHint     string          `json:"power_limit_hint,omitempty"`
	Env                hardwareTuneEnv `json:"env"`
	CPU                cpuTuneBlock    `json:"cpu"`
	Devices            []gpuTuneDevice `json:"devices"`
	Message            string          `json:"message,omitempty"`
}

type nvidiaPowerRow struct {
	Index   int
	DrawW   float64
	LimitW  float64
	MinW    float64
	MaxW    float64
	PowerOK bool
}

func clampf(v, lo, hi float64) float64 {
	if hi > 0 && v > hi {
		return hi
	}
	if lo > 0 && v < lo {
		return lo
	}
	return v
}

func powerTargetByPct(base, pct, minW, maxW float64) float64 {
	if base <= 0 {
		base = maxW
	}
	if base <= 0 {
		base = minW
	}
	if base <= 0 {
		return 0
	}
	return clampf(base*pct/100, minW, maxW)
}

func powerBaseForPreset(limitW, maxW float64, hints gputune.Hints) float64 {
	// Prefer current limit as baseline to avoid accidental "preset == near max power".
	if limitW > 0 {
		return limitW
	}
	if hints.TypicalTDPW > 0 {
		tdp := float64(hints.TypicalTDPW)
		if maxW > 0 && tdp > maxW {
			return maxW
		}
		return tdp
	}
	if maxW > 0 {
		return maxW
	}
	return 0
}

func enrichPowerTune(d *gpuTuneDevice) {
	minW := d.PowerMinW
	maxW := d.PowerMaxW
	limW := d.PowerLimitW
	if maxW <= 0 {
		maxW = limW
	}
	if minW <= 0 && maxW > 0 {
		minW = maxW * 0.5
	}
	recPct := float64(d.Hints.RecommendedPL)
	if recPct <= 0 {
		recPct = 82
	}
	if recPct > 100 {
		recPct = 100
	}
	base := powerBaseForPreset(limW, maxW, d.Hints)
	safeMinPct := float64(d.Hints.PLRangeMin)
	safeMaxPct := float64(d.Hints.PLRangeMax)
	if safeMinPct <= 0 {
		safeMinPct = 70
	}
	if safeMaxPct <= 0 {
		safeMaxPct = 92
	}
	if safeMinPct > safeMaxPct {
		safeMinPct, safeMaxPct = safeMaxPct, safeMinPct
	}

	d.SafeMinW = powerTargetByPct(base, safeMinPct, minW, maxW)
	d.SafeMaxW = powerTargetByPct(base, safeMaxPct, minW, maxW)
	d.TurboMaxW = powerTargetByPct(base, 97, minW, maxW)
	d.PresetEcoW = powerTargetByPct(base, recPct-10, minW, maxW)
	d.PresetDailyW = powerTargetByPct(base, recPct, minW, maxW)
	d.PresetTurboW = powerTargetByPct(base, recPct+8, minW, d.TurboMaxW)
	if d.PresetDailyW > 0 {
		d.PresetsAvailable = true
	}
	if d.PowerMaxW <= 0 && d.Hints.TypicalTDPW > 0 {
		tdp := float64(d.Hints.TypicalTDPW)
		d.PowerMinW = tdp * 0.5
		d.PowerMaxW = tdp
	}
}

func attachRigManualOC(dev *gpuTuneDevice) {
	if dev == nil || strings.TrimSpace(dev.Name) == "" {
		return
	}
	if pid := strings.TrimSpace(os.Getenv("HACKME_RIG_PROFILE")); pid != "" {
		if p, ok := gputune.GetRigProfile(pid); ok {
			dev.ManualOC = p.ManualOC
			return
		}
	}
	if p, ok := gputune.DetectRigProfile([]string{dev.Name}); ok {
		dev.ManualOC = p.ManualOC
	}
}

func finalizeTuneDevice(dev *gpuTuneDevice, idx int, driverMismatch bool) {
	enrichPowerTune(dev)
	attachRigManualOC(dev)
	dev.PresetsAvailable = dev.PresetDailyW > 0
	dev.DriverMismatch = driverMismatch
	if dev.PresetsAvailable {
		w := int(math.Round(dev.PresetDailyW))
		if w > 0 {
			dev.NvidiaSMIPlCommand = "nvidia-smi -i " + strconv.Itoa(idx) + " -pl " + strconv.Itoa(w)
		}
	}
}

func hostHasNVIDIAGPU() bool {
	return len(queryNVIDIAMulti()) > 0 || len(gpuhost.ListNVIDIAProcCards()) > 0
}

func cpuSoftCapHints() cpuTuneBlock {
	return cpuTuneBlock{
		SoftCapPct: 0,
		DefaultPct: chain.DefaultSoftCPUThrottlePct,
		MinPct:     1,
		MaxPct:     100,
		Tips: []string{
			"Soft CPU cap: PoH workers yield briefly when sustained load exceeds the target (not a BIOS overclock).",
			"OS power plan is separate; 100% effectively disables extra yield for this rule only.",
		},
		Note:         "Applies to the running miner; default at startup from env if set.",
		EnvVar:       "HACKME_MINER_CPU_PCT",
		EnvOverrides: "Env at process start seeds the default; POST changes the cap until restart.",
	}
}

// queryNVIDIAPowerRows returns per-GPU power stats; PowerOK false if query fails.
func queryNVIDIAPowerRows() []nvidiaPowerRow {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSMITimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi",
		"--query-gpu=index,power.draw,power.limit,power.min_limit,power.max_limit",
		"--format=csv,noheader,nounits")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var rows []nvidiaPowerRow
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 5 {
			continue
		}
		idx, err0 := strconv.Atoi(strings.TrimSpace(parts[0]))
		draw, err1 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		limit, err2 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		minW, err3 := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
		maxW, err4 := strconv.ParseFloat(strings.TrimSpace(parts[4]), 64)
		if err0 != nil {
			continue
		}
		r := nvidiaPowerRow{Index: idx, PowerOK: true}
		if err1 == nil {
			r.DrawW = draw
		}
		if err2 == nil {
			r.LimitW = limit
		}
		if err3 == nil {
			r.MinW = minW
		}
		if err4 == nil {
			r.MaxW = maxW
		}
		rows = append(rows, r)
	}
	return rows
}

func currentPowerRowForGPU(idx int) (nvidiaPowerRow, bool) {
	for _, p := range queryNVIDIAPowerRows() {
		if p.Index == idx {
			return p, true
		}
	}
	return nvidiaPowerRow{}, false
}

func waitAppliedPowerLimitW(idx int, targetW int, attempts int, wait time.Duration) (float64, bool) {
	if attempts < 1 {
		attempts = 1
	}
	for i := 0; i < attempts; i++ {
		row, ok := currentPowerRowForGPU(idx)
		if ok && row.LimitW > 0 {
			if math.Abs(row.LimitW-float64(targetW)) <= 1.0 {
				return row.LimitW, true
			}
			if i == attempts-1 {
				return row.LimitW, false
			}
		}
		time.Sleep(wait)
	}
	return 0, false
}

func envFloatTune(key string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return def
	}
	return x
}

func (a *app) buildHardwareTuneResponse() hardwareTuneResponse {
	cpu := cpuSoftCapHints()
	if a.miner != nil {
		cpu.SoftCapPct = a.miner.Stats().ThrottleCPUPct
	}
	nvRows := queryNVIDIAMulti()
	powerRows := queryNVIDIAPowerRows()
	amdList := gpuhost.ListAMDGPUTelemetry()
	procCards := gpuhost.ListNVIDIAProcCards()
	driverMismatch := len(powerRows) == 0 && len(procCards) > 0 && len(nvRows) > 0

	resp := hardwareTuneResponse{
		NvidiaSMI:        len(nvRows) > 0 || len(powerRows) > 0,
		AMDTelemetry:     len(amdList) > 0,
		CanSetPowerLimit: len(nvRows) > 0 || len(powerRows) > 0,
		Env: hardwareTuneEnv{
			GPUTempPauseC:  envFloatTune("HACKME_GPU_TEMP_PAUSE_C", 0),
			GPUTempResumeC: envFloatTune("HACKME_GPU_TEMP_RESUME_C", 0),
			Note:           "GPU thermal guard: HACKME_GPU_TEMP_PAUSE_C / HACKME_GPU_TEMP_RESUME_C (°C). 0 = off. Restart node after env changes.",
		},
		CPU:                cpu,
		ActiveRigProfileID: strings.TrimSpace(os.Getenv("HACKME_RIG_PROFILE")),
		DriverMismatch:     driverMismatch,
	}
	if runtime.GOOS == "linux" && os.Geteuid() != 0 {
		resp.PowerLimitHint = "Linux: nvidia-smi -pl often needs root or GPU-capable groups."
	}
	if resp.Env.GPUTempPauseC > 0 && resp.Env.GPUTempResumeC <= 0 {
		resp.Env.GPUTempResumeC = resp.Env.GPUTempPauseC - 10
		if resp.Env.GPUTempResumeC < 0 {
			resp.Env.GPUTempResumeC = 0
		}
	}

	powerByIdx := make(map[int]nvidiaPowerRow)
	for _, p := range powerRows {
		powerByIdx[p.Index] = p
	}

	if len(nvRows) > 0 {
		for _, nv := range nvRows {
			p := powerByIdx[nv.Index]
			dev := gpuTuneDevice{
				Index: nv.Index, Name: nv.Name, TempC: nv.TempC, UtilPct: nv.Util,
				PowerDrawW: p.DrawW, PowerLimitW: p.LimitW, PowerMinW: p.MinW, PowerMaxW: p.MaxW,
				Hints: gputune.ForGPUName(nv.Name), PowerReadable: p.PowerOK,
			}
			finalizeTuneDevice(&dev, nv.Index, driverMismatch)
			resp.Devices = append(resp.Devices, dev)
			if dev.PresetsAvailable {
				resp.PresetsAvailable = true
			}
		}
		if driverMismatch {
			resp.CanSetPowerLimit = false
			resp.PresetsAvailable = true
			resp.Message = "GPU detected (driver active). NVML/nvidia-smi power query unavailable — reboot after driver update for live telemetry. Eco/Daily/Turbo presets still apply via nvidia-smi -pl when permissions allow."
		}
		return resp
	}

	if len(amdList) > 0 {
		resp.CanSetPowerLimit = false
		for _, am := range amdList {
			util := -1.0
			if am.BusyPct >= 0 {
				util = am.BusyPct
			}
			dev := gpuTuneDevice{
				Index: am.Index, Name: am.Name, TempC: am.TempC, UtilPct: util,
				PowerDrawW: am.PowerDrawW, PowerLimitW: 0, PowerMinW: 0, PowerMaxW: 0,
				Hints: gputune.ForGPUName(am.Name), PowerReadable: am.PowerAverage,
			}
			finalizeTuneDevice(&dev, am.Index, false)
			resp.Devices = append(resp.Devices, dev)
			if dev.PresetsAvailable {
				resp.PresetsAvailable = true
			}
		}
		resp.Message = "AMD: temp/load from sysfs (amdgpu). Power limit via API is NVIDIA-only (nvidia-smi -pl); on AMD use vendor tools / BIOS."
		return resp
	}

	u, _, name, t := queryNVIDIA()
	if name != "" {
		p := powerByIdx[0]
		dev := gpuTuneDevice{
			Index: 0, Name: name, TempC: t, UtilPct: u,
			PowerDrawW: p.DrawW, PowerLimitW: p.LimitW, PowerMinW: p.MinW, PowerMaxW: p.MaxW,
			Hints: gputune.ForGPUName(name), PowerReadable: p.PowerOK,
		}
		finalizeTuneDevice(&dev, 0, driverMismatch)
		resp.Devices = append(resp.Devices, dev)
		if dev.PresetsAvailable {
			resp.PresetsAvailable = true
		}
		return resp
	}

	// NVIDIA driver loaded but nvidia-smi name query empty — still show proc cards.
	if len(procCards) > 0 {
		for _, c := range procCards {
			dev := gpuTuneDevice{
				Index: c.Index, Name: c.Name, TempC: -1, UtilPct: -1,
				Hints: gputune.ForGPUName(c.Name),
			}
			finalizeTuneDevice(&dev, c.Index, true)
			resp.Devices = append(resp.Devices, dev)
			resp.PresetsAvailable = true
		}
		resp.DriverMismatch = true
		resp.Message = "GPU via /proc/driver/nvidia. Reboot after driver update, then Eco/Daily/Turbo and nvidia-smi -pl work with telemetry."
		return resp
	}

	resp.Message = "No GPU telemetry: no nvidia-smi and no amdgpu sysfs on this host."
	return resp
}

func (a *app) handleHardwareTune(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(a.buildHardwareTuneResponse())
	case http.MethodPost:
		if !requireAdminAuth(w, r) {
			return
		}
		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		_, hasSoft := raw["soft_cap_pct"]
		_, hasGPU := raw["gpu_index"]
		_, hasPL := raw["power_limit_w"]
		if hasSoft && (hasGPU || hasPL) {
			http.Error(w, "use either soft_cap_pct or gpu power fields, not both", http.StatusBadRequest)
			return
		}
		if hasSoft {
			var in struct {
				SoftCapPct float64 `json:"soft_cap_pct"`
			}
			b, _ := json.Marshal(raw)
			if err := json.Unmarshal(b, &in); err != nil {
				http.Error(w, "invalid json", http.StatusBadRequest)
				return
			}
			if a.miner == nil {
				http.Error(w, "miner unavailable", http.StatusInternalServerError)
				return
			}
			if err := a.miner.SetSoftCPUThrottlePct(in.SoftCapPct); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			log.Printf("hardware tune: soft CPU cap set to %.1f%%", in.SoftCapPct)
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":           true,
				"soft_cap_pct": in.SoftCapPct,
			})
			return
		}
		if (hasGPU || hasPL) && !hostHasNVIDIAGPU() && len(gpuhost.ListAMDGPUTelemetry()) > 0 {
			http.Error(w, "GPU power limit and presets require NVIDIA (nvidia-smi). AMD: use Radeon Software, CoreCtrl, or BIOS power limits.", http.StatusBadRequest)
			return
		}
		var body struct {
			GPUIndex    int     `json:"gpu_index"`
			PowerLimitW float64 `json:"power_limit_w"`
			Mode        string  `json:"mode"` // eco | daily | turbo
		}
		b, _ := json.Marshal(raw)
		if err := json.Unmarshal(b, &body); err != nil {
			http.Error(w, "invalid json", http.StatusBadRequest)
			return
		}
		if body.GPUIndex < 0 {
			http.Error(w, "expected soft_cap_pct (1..100) or gpu_index>=0 with power_limit_w>0", http.StatusBadRequest)
			return
		}
		mode := strings.ToLower(strings.TrimSpace(body.Mode))
		powerRows := queryNVIDIAPowerRows()
		var selected *nvidiaPowerRow
		for i := range powerRows {
			if powerRows[i].Index == body.GPUIndex {
				selected = &powerRows[i]
				break
			}
		}
		if mode != "" && body.PowerLimitW <= 0 {
			if mode != "eco" && mode != "daily" && mode != "turbo" {
				http.Error(w, "mode must be eco|daily|turbo", http.StatusBadRequest)
				return
			}
			for _, d := range a.buildHardwareTuneResponse().Devices {
				if d.Index != body.GPUIndex {
					continue
				}
				switch mode {
				case "eco":
					body.PowerLimitW = d.PresetEcoW
				case "turbo":
					body.PowerLimitW = d.PresetTurboW
				default:
					body.PowerLimitW = d.PresetDailyW
				}
				break
			}
			if body.PowerLimitW <= 0 {
				nvRows := queryNVIDIAMulti()
				gpuName := ""
				for _, nv := range nvRows {
					if nv.Index == body.GPUIndex {
						gpuName = strings.TrimSpace(nv.Name)
						break
					}
				}
				if gpuName == "" {
					for _, c := range gpuhost.ListNVIDIAProcCards() {
						if c.Index == body.GPUIndex {
							gpuName = c.Name
							break
						}
					}
				}
				h := gputune.ForGPUName(gpuName)
				base := 0.0
				if selected != nil {
					base = powerBaseForPreset(selected.LimitW, selected.MaxW, h)
				} else if h.TypicalTDPW > 0 {
					base = float64(h.TypicalTDPW)
				}
				rec := float64(h.RecommendedPL)
				if rec <= 0 {
					rec = 82
				}
				targetPct := rec
				if mode == "eco" {
					targetPct = rec - 10
				} else if mode == "turbo" {
					targetPct = rec + 8
				}
				minW, maxW := 0.0, 0.0
				if selected != nil {
					minW, maxW = selected.MinW, selected.MaxW
					if mode == "turbo" && maxW > 0 {
						maxW = maxW * 0.97
					}
				} else if h.TypicalTDPW > 0 {
					maxW = float64(h.TypicalTDPW)
					minW = maxW * 0.5
				}
				body.PowerLimitW = powerTargetByPct(base, targetPct, minW, maxW)
			}
		}
		if body.PowerLimitW <= 0 {
			http.Error(w, "expected soft_cap_pct (1..100) or gpu_index>=0 with power_limit_w>0", http.StatusBadRequest)
			return
		}
		if selected != nil {
			minW, maxW := selected.MinW, selected.MaxW
			if minW > 0 && body.PowerLimitW < minW {
				http.Error(w, "power_limit_w below driver min ("+strconv.FormatFloat(minW, 'f', 0, 64)+"W)", http.StatusBadRequest)
				return
			}
			if maxW > 0 && body.PowerLimitW > maxW {
				http.Error(w, "power_limit_w above driver max ("+strconv.FormatFloat(maxW, 'f', 0, 64)+"W)", http.StatusBadRequest)
				return
			}
		}
		targetW := int(math.Round(body.PowerLimitW))
		if targetW <= 0 {
			http.Error(w, "power_limit_w must be > 0", http.StatusBadRequest)
			return
		}
		beforeRow, hasBefore := currentPowerRowForGPU(body.GPUIndex)
		if hasBefore && beforeRow.LimitW > 0 && math.Abs(beforeRow.LimitW-float64(targetW)) <= 1.0 {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"ok":                      true,
				"message":                 "power limit already set",
				"requested_power_limit_w": targetW,
				"applied_power_limit_w":   beforeRow.LimitW,
				"note":                    "Limit already matched request; draw/hashrate changes show under load.",
			})
			return
		}
		cmd := exec.Command("nvidia-smi", "-i", strconv.Itoa(body.GPUIndex), "-pl", strconv.Itoa(targetW))
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		if err := cmd.Run(); err != nil {
			detail := strings.TrimSpace(out.String())
			log.Printf("hardware tune: nvidia-smi -pl: %v: %s", err, detail)
			code := "nvidia_smi_error"
			hint := "Windows: run elevated if -pl is rejected; confirm driver supports power limits."
			lower := strings.ToLower(detail)
			if strings.Contains(lower, "insufficient permissions") || strings.Contains(lower, "not permitted") {
				code = "insufficient_permissions"
				hint = "Insufficient privileges for nvidia-smi -pl (Linux: run node with sudo or correct groups)."
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"code":   code,
				"error":  err.Error(),
				"detail": detail,
				"hint":   hint,
			})
			return
		}
		// Re-read actual power limit with short retries to avoid race with driver state update.
		appliedLimitW, exactApplied := waitAppliedPowerLimitW(body.GPUIndex, targetW, 6, 200*time.Millisecond)
		log.Printf("hardware tune: set GPU %d power limit requested=%dW applied=%.0fW (ok)", body.GPUIndex, targetW, appliedLimitW)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		resp := map[string]any{
			"ok":                      true,
			"message":                 strings.TrimSpace(out.String()),
			"requested_power_limit_w": targetW,
			"applied_power_limit_w":   appliedLimitW,
			"note":                    "Power limit affects draw/hashrate under load; idle may look unchanged.",
		}
		if appliedLimitW > 0 && !exactApplied {
			resp["warning"] = "driver_clamped_or_ignored_request"
			resp["hint"] = "Check driver min/max and persistence mode; some GPUs clamp -pl."
		}
		if appliedLimitW <= 0 {
			resp["warning"] = "applied_power_unverified"
			resp["hint"] = "Could not re-read power limit after apply; verify with nvidia-smi."
		}
		_ = json.NewEncoder(w).Encode(resp)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
