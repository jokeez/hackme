//go:build !linux

package gpuhost

import "strings"

type oclGPU struct {
	Index int
	Name  string
}

func platformGPUInventory(forceOpenCL bool) (cudaCount int, ocl []oclGPU) {
	rep := detectHostGPUs()
	cudaCount = 0
	if rep.HasNVIDIA {
		cudaCount = len(queryNVIDIAWindows())
		if cudaCount == 0 {
			cudaCount = 1
		}
	}
	idx := 0
	for _, name := range rep.Names {
		n := strings.ToLower(name)
		if strings.Contains(n, "amd") || strings.Contains(n, "radeon") || strings.Contains(n, "intel") {
			if strings.Contains(n, "nvidia") || strings.Contains(n, "geforce") {
				continue
			}
			ocl = append(ocl, oclGPU{Index: idx, Name: name})
			idx++
		}
	}
	if rep.HasAMD && len(ocl) == 0 {
		ocl = append(ocl, oclGPU{Index: 0, Name: "AMD GPU"})
	}
	_ = forceOpenCL
	return cudaCount, ocl
}

func nvidiaNameAt(index int) string {
	names := queryNVIDIAWindows()
	if index >= 0 && index < len(names) {
		return names[index]
	}
	return ""
}

func queryNVIDIAWindows() []string {
	rep := detectHostGPUs()
	var out []string
	for _, n := range rep.Names {
		nv, _, _ := classifyName(n)
		if nv {
			out = append(out, n)
		}
	}
	return out
}
