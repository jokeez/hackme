package gpupoh

import (
	"context"
	"errors"
)

// Accelerator is one GPU PoH search backend (CUDA or OpenCL).
type Accelerator interface {
	DeviceIndex() int
	DeviceName() string
	Backend() string
	// Label is a stable UI string, e.g. "GPU #0 [CUDA]".
	Label() string
	Search(ctx context.Context, base, count, mod uint64) (found bool, nonce uint64, err error)
	Close() error
}

// ErrNoGPU is returned when no GPU backend could be initialized.
var ErrNoGPU = errors.New("gpupoh: no GPU accelerators available")

// GPUDeviceInfo is a lightweight device listing (no contexts created for CUDA).
type GPUDeviceInfo struct {
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Backend string `json:"backend"` // "cuda" or "opencl"
}
