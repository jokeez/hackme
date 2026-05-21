//go:build !linux && !windows

package gpuhost

func detectHostGPUs() HostGPUReport {
	return finalizeHostReport(HostGPUReport{})
}
