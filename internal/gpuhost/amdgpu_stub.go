//go:build !linux

package gpuhost

// AMDGPUCardTelemetry is populated on Linux via sysfs (amdgpu DRM).
type AMDGPUCardTelemetry struct {
	Index        int
	Card         string
	Name         string
	TempC        float64
	BusyPct      float64 // >=0 when read from gpu_busy_percent; -1 when unavailable
	PowerDrawW   float64
	PowerAverage bool
}

// ListAMDGPUTelemetry is Linux-only (sysfs amdgpu).
func ListAMDGPUTelemetry() []AMDGPUCardTelemetry { return nil }
