package gpuhost

// PoHGPUTemps returns GPU ordinal → temperature °C for mining thermal control.
// NVIDIA indices match nvidia-smi; on Linux, amdgpu sysfs fills slots when NVIDIA is absent
// or adds indices not reported by nvidia-smi (unusual hybrid setups).
func PoHGPUTemps() map[int]float64 {
	nv := NVIDIAGPUTemps()
	am := amdgpuTempMapFromList(ListAMDGPUTelemetry())
	if len(nv) == 0 {
		return am
	}
	out := make(map[int]float64)
	for k, v := range nv {
		out[k] = v
	}
	for k, v := range am {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	return out
}

func amdgpuTempMapFromList(rows []AMDGPUCardTelemetry) map[int]float64 {
	m := make(map[int]float64, len(rows))
	for _, r := range rows {
		if r.TempC > 0 {
			m[r.Index] = r.TempC
		}
	}
	return m
}
