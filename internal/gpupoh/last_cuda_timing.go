//go:build cuda

package gpupoh

import "sync/atomic"

var lastCUDAKernelNS int64

func recordCUDAKernelDuration(sec float64) {
	if sec <= 0 {
		return
	}
	atomic.StoreInt64(&lastCUDAKernelNS, int64(sec*1e9))
}

// LastCUDAKernelSeconds returns wall time of the most recent CUDA kernel (post-Synchronize).
func LastCUDAKernelSeconds() float64 {
	return float64(atomic.LoadInt64(&lastCUDAKernelNS)) / 1e9
}
