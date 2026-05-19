//go:build opencl

package gpupoh

import "sync/atomic"

var lastOCLKernelNS int64

func recordOCLKernelDuration(sec float64) {
	if sec <= 0 {
		return
	}
	atomic.StoreInt64(&lastOCLKernelNS, int64(sec*1e9))
}

// LastOCLKernelSeconds returns wall time of the most recent OpenCL kernel (post-clFinish).
func LastOCLKernelSeconds() float64 {
	return float64(atomic.LoadInt64(&lastOCLKernelNS)) / 1e9
}
