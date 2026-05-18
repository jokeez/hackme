//go:build !cuda

package gpupoh

func listCUDAGPUDevices() []GPUDeviceInfo { return nil }
