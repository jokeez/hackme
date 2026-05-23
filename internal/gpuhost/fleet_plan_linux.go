//go:build linux

package gpuhost

import (
	"os/exec"
	"strings"
)

type oclGPU struct {
	Index int
	Name  string
}

// platformGPUInventory returns NVIDIA count and OpenCL GPU slots (AMD/Intel preferred; skips NVIDIA OCL duplicates in hybrid).
func platformGPUInventory(forceOpenCL bool) (cudaCount int, ocl []oclGPU) {
	cudaCount = countNVIDIAGPUs()
	ocl = listOpenCLDiscreteGPUs(forceOpenCL)
	if cudaCount > 0 && len(ocl) > 0 {
		// Hybrid: mine AMD/Intel on OpenCL only; NVIDIA uses CUDA workers.
		var amd []oclGPU
		for _, g := range ocl {
			n := strings.ToLower(g.Name)
			if strings.Contains(n, "nvidia") || strings.Contains(n, "geforce") || strings.Contains(n, "quadro") {
				continue
			}
			amd = append(amd, g)
		}
		if len(amd) > 0 {
			ocl = amd
		}
	}
	return cudaCount, ocl
}

func countNVIDIAGPUs() int {
	out, err := exec.Command("nvidia-smi", "-L").Output()
	if err != nil {
		return 0
	}
	n := 0
	for _, ln := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "GPU ") {
			n++
		}
	}
	return n
}

func nvidiaNameAt(index int) string {
	out, err := exec.Command("nvidia-smi", "--query-gpu=name", "--format=csv,noheader").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if index >= 0 && index < len(lines) {
		return strings.TrimSpace(lines[index])
	}
	return ""
}

func listOpenCLDiscreteGPUs(forceOpenCL bool) []oclGPU {
	if out, err := exec.Command("clinfo").Output(); err == nil && len(out) > 0 {
		return parseClinfoGPUs(string(out))
	}
	// sysfs fallback: AMD / Intel cards only
	var gpus []oclGPU
	idx := 0
	for _, c := range ListAMDGPUTelemetry() {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = "AMD GPU"
		}
		gpus = append(gpus, oclGPU{Index: idx, Name: name})
		idx++
	}
	_ = forceOpenCL
	return gpus
}

func parseClinfoGPUs(text string) []oclGPU {
	var gpus []oclGPU
	lines := strings.Split(text, "\n")
	var curName string
	var curType string
	deviceIdx := -1
	flush := func() {
		if deviceIdx < 0 {
			return
		}
		if curType == "" || !strings.Contains(strings.ToLower(curType), "gpu") {
			return
		}
		name := strings.TrimSpace(curName)
		if name == "" {
			name = "OpenCL GPU"
		}
		gpus = append(gpus, oclGPU{Index: deviceIdx, Name: name})
	}
	for _, ln := range lines {
		t := strings.TrimSpace(ln)
		low := strings.ToLower(t)
		if strings.HasPrefix(low, "device #") {
			flush()
			deviceIdx++
			curName = ""
			curType = ""
			continue
		}
		if deviceIdx < 0 {
			continue
		}
		if strings.HasPrefix(low, "device name") {
			if i := strings.Index(t, ":"); i >= 0 {
				curName = strings.TrimSpace(t[i+1:])
			}
		}
		if strings.HasPrefix(low, "device type") {
			if i := strings.Index(t, ":"); i >= 0 {
				curType = strings.TrimSpace(t[i+1:])
			}
		}
	}
	flush()
	return gpus
}
