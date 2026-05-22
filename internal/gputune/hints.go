// Package gputune provides heuristic GPU tuning hints (not hardware-specific advice).
package gputune

import (
	"strings"
)

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

// Hints are approximate suggestions for dashboard display only.
type Hints struct {
	Family        string   `json:"family"`
	Vendor        string   `json:"vendor,omitempty"`
	TypicalTDPW   int      `json:"typical_tdp_w,omitempty"`
	RecommendedPL int      `json:"recommended_pl_pct,omitempty"`
	PLRangeMin    int      `json:"pl_range_min_pct,omitempty"`
	PLRangeMax    int      `json:"pl_range_max_pct,omitempty"`
	Tips          []string `json:"tips"`
	Undervolt     string   `json:"undervolt_note"`
	ManualTools   string   `json:"manual_tools"`
	OfficialPath  string   `json:"official_path,omitempty"`
	PowerCapNote  string   `json:"power_cap_note,omitempty"`
}

// ForGPUName returns heuristic hints based on the driver-reported GPU name.
func ForGPUName(name string) Hints {
	n := strings.ToLower(strings.TrimSpace(name))
	h := Hints{
		Family:        "Unknown",
		Vendor:        "Generic",
		RecommendedPL: 85,
		PLRangeMin:    70,
		PLRangeMax:    95,
		Tips:          []string{"Stability over peak hashrate: change one knob at a time and stress-test."},
		Undervolt:     "Many GPUs benefit from a lower power limit or a mild voltage curve in vendor tools — values are per-card.",
		ManualTools:   "NVIDIA: driver panel, nvidia-smi -pl (W cap). AMD: Radeon Software. Fine curves: MSI Afterburner / Adrenalin where applicable.",
		OfficialPath:  "NVIDIA: NVIDIA App / Control Panel, then nvidia-smi -pl; AMD: Radeon Software performance tuning.",
		PowerCapNote:  "If available, try −10–15% power limit first and watch temps + stability.",
	}

	switch {
	case containsAny(n, "h100", "h200", "b200", "b100", "hopper", "blackwell datacenter"):
		h.Family = "Hopper"
		h.Vendor = "NVIDIA"
		h.TypicalTDPW = 700
		h.RecommendedPL = 90
		h.PLRangeMin = 80
		h.PLRangeMax = 100
		h.Tips = append(h.Tips,
			"Datacenter accelerators: use data-center driver branch and nvidia-smi persistence mode.",
			"HMAI / vector workloads: cap batch growth until thermals and ECC are verified.",
		)
	case containsAny(n, "rtx 50", "rtx 5090", "rtx 5080", "rtx 5070", "rtx 5060", "rtx 5050"):
		h.Family = "Blackwell (hint)"
		h.Vendor = "NVIDIA"
		h.TypicalTDPW = 350
		h.RecommendedPL = 82
		h.PLRangeMin = 70
		h.PLRangeMax = 90
		h.Tips = append(h.Tips,
			"Newer gens are sensitive to cooling and power cap — start with PL −10…15%.",
			"After any clock change, verify stability; compute artifacts are not “games only”.",
		)
	case containsAny(n, "rtx 40", "rtx 4090", "rtx 4080", "rtx 4070", "rtx 4060"):
		h.Family = "Ada Lovelace"
		h.Vendor = "NVIDIA"
		h.TypicalTDPW = 320
		h.RecommendedPL = 80
		h.PLRangeMin = 68
		h.PLRangeMax = 90
		h.Tips = append(h.Tips,
			"Undervolt / lower PL often saves watts with similar hashrate.",
			"GDDR6X VRAM: watch memory thermals under sustained load; case airflow matters.",
		)
	case containsAny(n, "rtx 30", "rtx 3090", "rtx 3080", "rtx 3070", "rtx 3060", "a40", "a5000"):
		h.Family = "Ampere"
		h.Vendor = "NVIDIA"
		h.TypicalTDPW = 350
		h.RecommendedPL = 78
		h.PLRangeMin = 65
		h.PLRangeMax = 88
		h.Tips = append(h.Tips,
			"Sustained load can heat VRAM — monitor hotspot temps if exposed by tools.",
			"Lower voltage curve or PL gradually; aim for stable clocks without thermal throttle.",
		)
	case containsAny(n, "rtx 20", "rtx 2080", "rtx 2070", "rtx 2060", "gtx 16", "gtx 1660", "titan rtx", "quadro rtx"):
		h.Family = "Turing"
		h.Vendor = "NVIDIA"
		h.TypicalTDPW = 220
		h.RecommendedPL = 82
		h.PLRangeMin = 70
		h.PLRangeMax = 92
		h.Tips = append(h.Tips,
			"Capping PL often beats aggressive core OC for watts and stability.",
		)
	case containsAny(n, "gtx 10", "gtx 1080", "gtx 1070", "gtx 1060", "p100", "p40", "tesla p"):
		h.Family = "Pascal"
		h.Vendor = "NVIDIA"
		h.TypicalTDPW = 180
		h.RecommendedPL = 86
		h.PLRangeMin = 75
		h.PLRangeMax = 95
		h.Tips = append(h.Tips,
			"Older cards: dust cleaning and paste often beat +50 MHz for throttling.",
		)
	case containsAny(n, "rx 7900", "rx 7800", "rx 7700", "rx 7600", "w7900", "w7800"):
		h.Family = "AMD RDNA 3"
		h.Vendor = "AMD"
		h.TypicalTDPW = 300
		h.RecommendedPL = 84
		h.PLRangeMin = 70
		h.PLRangeMax = 94
		h.Tips = append(h.Tips,
			"RDNA3 often responds well to mild undervolt and a daily PL cap.",
			"Long compute runs: watch hotspot and VRAM if the driver exposes them.",
		)
		h.ManualTools = "AMD: Radeon Software -> Performance -> Tuning. Linux: rocm-smi / amdgpu metrics where available."
		h.OfficialPath = "AMD Adrenalin: Performance > Tuning (manual profiles + stress)."
	case containsAny(n, "rx 6950", "rx 6900", "rx 6800", "rx 6750", "rx 6700", "rx 6650", "rx 6600", "rx 6500", "rx 6400", "w6800"):
		h.Family = "AMD RDNA 2"
		h.Vendor = "AMD"
		h.TypicalTDPW = 230
		h.RecommendedPL = 85
		h.PLRangeMin = 70
		h.PLRangeMax = 95
		h.Tips = append(h.Tips,
			"AMD: Radeon Software undervolt (Advanced) — values are per sample.",
			"OpenCL on AMD: watch junction / hotspot if shown.",
		)
		h.ManualTools = "AMD: Radeon Software → Performance → Tuning; max power cap if available."
		h.OfficialPath = "AMD: Radeon Software tuning, then long stress under compute load."
	case containsAny(n, "rx 5700", "rx 5600", "rx 5500", "radeon vii"):
		h.Family = "AMD RDNA 1"
		h.Vendor = "AMD"
		h.TypicalTDPW = 210
		h.RecommendedPL = 86
		h.PLRangeMin = 72
		h.PLRangeMax = 95
	case containsAny(n, "rx 590", "rx 580", "rx 570", "rx 560", "vega 64", "vega 56"):
		h.Family = "AMD Polaris/Vega"
		h.Vendor = "AMD"
		h.TypicalTDPW = 220
		h.RecommendedPL = 88
		h.PLRangeMin = 75
		h.PLRangeMax = 96
		h.Tips = append(h.Tips,
			"Older AMD: memory clocks and airflow often matter more than core OC.",
		)
	case containsAny(n, "arc a770", "arc a750", "arc a580", "arc a380", "intel arc"):
		h.Family = "Intel Arc"
		h.Vendor = "Intel"
		h.TypicalTDPW = 225
		h.RecommendedPL = 90
		h.PLRangeMin = 78
		h.PLRangeMax = 98
		h.ManualTools = "Intel Arc Control / Intel Graphics Command Center (where available)."
		h.OfficialPath = "Intel Arc Control -> performance tuning with step-by-step stress testing."
	default:
		if n != "" {
			h.Tips = append(h.Tips, "Unrecognized name string — use vendor utilities and power limits.")
		}
	}
	return h
}
