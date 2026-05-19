//go:build !opencl

package gpupoh

// LastOCLKernelSeconds is only populated in OpenCL builds.
func LastOCLKernelSeconds() float64 { return 0 }
