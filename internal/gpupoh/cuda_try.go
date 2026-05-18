//go:build cuda

package gpupoh

import (
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
	ptx, kname, err := compilePTXForPoH()
	if err != nil {
		return nil, err
	}
	var out []Accelerator
	for i := 0; i < n; i++ {
		a, err := newCUDAAccelerator(i, ptx, kname)
		if err != nil {
			// Be resilient across heterogeneous rigs: skip bad devices,
			// continue with the GPUs that initialized successfully.
			continue
		}
		out = append(out, a)
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
