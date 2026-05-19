//go:build cuda

package gpupoh

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gorgonia.org/cu"
)

func nvrtcArchChainForDevice(devID int) ([]string, error) {
	dev, err := cu.GetDevice(devID)
	if err != nil {
		return nil, err
	}
	major, minor, err := dev.ComputeCapability()
	if err != nil {
		return nvrtcArchChain(0, 0), nil
	}
	return nvrtcArchChain(major, minor), nil
}

// cudaDeviceSummary is a one-line description for logs and diagnostics.
func cudaDeviceSummary(devID int) string {
	dev, err := cu.GetDevice(devID)
	if err != nil {
		return fmt.Sprintf("CUDA #%d (unknown)", devID)
	}
	name, _ := dev.Name()
	major, minor, _ := dev.ComputeCapability()
	mem, _ := dev.TotalMem()
	arch := cudaComputeArch(major, minor)
	if mem > 0 {
		return fmt.Sprintf("CUDA #%d %s CC %d.%d (%s) %.1f GiB", devID, name, major, minor, arch, float64(mem)/(1<<30))
	}
	return fmt.Sprintf("CUDA #%d %s CC %d.%d (%s)", devID, name, major, minor, arch)
}

// envCUDABlockThreads allows tuning block size (default 256).
func envCUDABlockThreads() int {
	v := strings.TrimSpace(os.Getenv("HACKME_CUDA_BLOCK_THREADS"))
	if v == "" {
		return blockThreads
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 32 || n > 1024 {
		return blockThreads
	}
	return n
}
