//go:build linux

package gpuhost

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func detectHostGPUs() HostGPUReport {
	var rep HostGPUReport

	for _, c := range ListNVIDIAProcCards() {
		if strings.TrimSpace(c.Name) != "" {
			mergeReportNames(&rep, c.Name)
		}
	}
	for _, c := range ListAMDGPUTelemetry() {
		if strings.TrimSpace(c.Name) != "" {
			mergeReportNames(&rep, c.Name)
		}
	}
	for _, n := range lspciGPUNames() {
		mergeReportNames(&rep, n)
	}
	for _, n := range drmVendorGPUNames() {
		mergeReportNames(&rep, n)
	}
	if len(rep.Names) == 0 {
		if n := nvidiaSMILineName(); n != "" {
			mergeReportNames(&rep, n)
		}
	}
	return finalizeHostReport(rep)
}

func lspciGPUNames() []string {
	out, err := exec.Command("lspci").Output()
	if err != nil || len(out) == 0 {
		return nil
	}
	var names []string
	for _, ln := range strings.Split(string(out), "\n") {
		l := strings.TrimSpace(ln)
		low := strings.ToLower(l)
		if !strings.Contains(low, "vga compatible controller") &&
			!strings.Contains(low, "3d controller") &&
			!strings.Contains(low, "display controller") {
			continue
		}
		parts := strings.SplitN(l, ":", 3)
		if len(parts) < 3 {
			continue
		}
		model := strings.TrimSpace(parts[2])
		for {
			i := strings.Index(model, "[")
			j := strings.Index(model, "]")
			if i >= 0 && j > i {
				model = strings.TrimSpace(model[:i] + model[j+1:])
				continue
			}
			break
		}
		model = strings.Join(strings.Fields(model), " ")
		if model != "" && !strings.Contains(strings.ToLower(model), "device ") {
			names = append(names, model)
		}
	}
	return names
}

func drmVendorGPUNames() []string {
	vendorLabel := map[string]string{
		"0x10de": "NVIDIA GPU",
		"4098":   "AMD GPU",
		"0x1002": "AMD GPU",
		"0x8086": "Intel GPU",
		"32902":  "Intel GPU",
	}
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var names []string
	for _, ent := range entries {
		name := ent.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}
		vendorPath := filepath.Join("/sys/class/drm", name, "device", "vendor")
		raw, err := os.ReadFile(vendorPath)
		if err != nil {
			continue
		}
		v := strings.TrimSpace(string(raw))
		label, ok := vendorLabel[v]
		if !ok {
			continue
		}
		key := strings.ToLower(label + name)
		if seen[key] {
			continue
		}
		seen[key] = true
		names = append(names, label)
	}
	return names
}

func nvidiaSMILineName() string {
	out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[0])
}
