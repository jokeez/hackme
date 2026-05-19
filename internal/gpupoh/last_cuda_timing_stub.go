//go:build !cuda

package gpupoh

// LastCUDAKernelSeconds is only populated in CUDA builds.
func LastCUDAKernelSeconds() float64 { return 0 }
