//go:build cuda || opencl

package gpupoh

import (
	"os"
	"strings"
)

// DiscoverAccelerators prefers CUDA when built and devices exist, unless
// HACKME_FORCE_OPENCL=1 (then OpenCL only). OpenCL is used when no CUDA devices
// or when the binary is opencl-only.
func DiscoverAccelerators() ([]Accelerator, error) {
	if forceOpenCL() {
		return tryOpenCLAccelerators()
	}
	cudaAcc, err := tryCUDAAccelerators()
	if err != nil {
		return nil, err
	}
	if len(cudaAcc) > 0 {
		return cudaAcc, nil
	}
	return tryOpenCLAccelerators()
}

func forceOpenCL() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_FORCE_OPENCL"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// BackendTag is a coarse build hint ("gpu"); live metrics use per-device Backend().
func BackendTag() string { return "gpu" }
