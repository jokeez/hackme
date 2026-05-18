//go:build !opencl

package gpupoh

func listOpenCLGPUDevices() []GPUDeviceInfo { return nil }
