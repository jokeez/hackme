//go:build cuda

package gpupoh

import (
	"fmt"
	"os"
	"strings"

	"github.com/pkg/errors"
	"gorgonia.org/cu"
)

// tryCUDAAccelerators builds one Accelerator per visible CUDA device.
func tryCUDAAccelerators() ([]Accelerator, error) {
	n, err := cu.NumDevices()
	if err != nil {
		return nil, errors.Wrap(err, "gpupoh: NumDevices")
	}
	if n < 1 {
		return nil, nil
	}
	var out []Accelerator
	var initLog []string
	for i := 0; i < n; i++ {
		if i >= MaxGPUDevices {
			break
		}
		a, err := newCUDAAccelerator(i)
		if err != nil {
			initLog = append(initLog, fmt.Sprintf("#%d: %v", i, err))
			continue
		}
		initLog = append(initLog, cudaDeviceSummary(i)+" → "+a.Label())
		out = append(out, a)
	}
	if len(out) == 0 && len(initLog) > 0 {
		return nil, fmt.Errorf("gpupoh: no CUDA device initialized (%s)", strings.Join(initLog, "; "))
	}
	if len(out) > 0 && os.Getenv("HACKME_CUDA_VERBOSE") == "1" {
		fmt.Fprintf(os.Stderr, "gpupoh: CUDA devices: %s\n", strings.Join(initLog, "; "))
	}
	return out, nil
}

// listCUDAGPUDevices returns visible CUDA GPUs (names only).
func listCUDAGPUDevices() []GPUDeviceInfo {
	n, err := cu.NumDevices()
	if err != nil || n < 1 {
		return nil
	}
	var out []GPUDeviceInfo
	for i := 0; i < n; i++ {
		dev, err := cu.GetDevice(i)
		if err != nil {
			continue
		}
		name, _ := dev.Name()
		out = append(out, GPUDeviceInfo{Index: i, Name: name, Backend: "cuda"})
	}
	return out
}
