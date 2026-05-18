//go:build !cuda && !opencl

package gpupoh

// DiscoverAccelerators is unavailable without cuda or opencl build tags.
func DiscoverAccelerators() ([]Accelerator, error) {
	return nil, ErrNoGPU
}

// BackendTag returns "cpu" when no GPU backends are compiled in.
func BackendTag() string { return "cpu" }

// GetAllGPUDevices is only populated when built with cuda and/or opencl tags.
func GetAllGPUDevices() ([]GPUDeviceInfo, error) {
	return nil, nil
}
