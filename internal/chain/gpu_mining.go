package chain

// GPUMiningDeviceStat is per-GPU PoH telemetry for /api/metrics and the dashboard.
type GPUMiningDeviceStat struct {
	Index       int     `json:"index"`
	Backend     string  `json:"backend"` // cuda | opencl
	Label       string  `json:"label"`   // e.g. GPU #0 [CUDA]
	Name        string  `json:"name"`    // marketing / driver name
	HashrateGHS float64 `json:"hashrate_gh_s"`
	TempC       float64 `json:"temp_c"` // -1 when unknown; Linux amdgpu sysfs + nvidia-smi when available
}
