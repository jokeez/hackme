//go:build cuda || opencl

package gpupoh

import (
	"fmt"
	"os"
	"strings"
)

func preferOpenCLFromEnv() bool {
	if forceOpenCL() {
		return true
	}
	b := strings.ToLower(strings.TrimSpace(os.Getenv("HACKME_GPU_BACKEND")))
	return b == "opencl"
}

// DiscoverAccelerators prefers CUDA when built and devices exist, unless
// HACKME_FORCE_OPENCL=1 or HACKME_GPU_BACKEND=opencl (then OpenCL only). OpenCL is
// used when no CUDA devices or when the binary is opencl-only.
func DiscoverAccelerators() ([]Accelerator, error) {
	if preferOpenCLFromEnv() {
		return tryOpenCLAccelerators()
	}
	cudaAcc, err := tryCUDAAccelerators()
	if err == nil && len(cudaAcc) > 0 {
		return cudaAcc, nil
	}
	if err != nil && os.Getenv("HACKME_CUDA_VERBOSE") == "1" {
		fmt.Fprintf(os.Stderr, "gpupoh: CUDA unavailable (%v); trying OpenCL\n", err)
	}
	ocl, oclErr := tryOpenCLAccelerators()
	if oclErr != nil {
		if err != nil {
			return nil, fmt.Errorf("gpupoh: cuda failed (%v); opencl failed (%v)", err, oclErr)
		}
		return nil, oclErr
	}
	if len(ocl) > 0 {
		return ocl, nil
	}
	if err != nil {
		return nil, err
	}
	return ocl, nil
}

func forceOpenCL() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_FORCE_OPENCL"))
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

// BackendTag is a coarse build hint ("gpu"); live metrics use per-device Backend().
func BackendTag() string { return "gpu" }
