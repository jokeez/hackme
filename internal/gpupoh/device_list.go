//go:build cuda || opencl

package gpupoh

// GetAllGPUDevices lists GPUs using the same preference order as DiscoverAccelerators
// (CUDA first unless HACKME_FORCE_OPENCL=1, then OpenCL).
func GetAllGPUDevices() ([]GPUDeviceInfo, error) {
	if forceOpenCL() {
		return listOpenCLGPUDevices(), nil
	}
	if d := listCUDAGPUDevices(); len(d) > 0 {
		return d, nil
	}
	return listOpenCLGPUDevices(), nil
}
