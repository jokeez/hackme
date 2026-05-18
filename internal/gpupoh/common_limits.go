//go:build cuda || opencl

package gpupoh

const (
	// MaxGPUDevices is the upper bound of parallel PoH GPU workers per node.
	MaxGPUDevices = 16
	maxBatch      = 1 << 26
)
