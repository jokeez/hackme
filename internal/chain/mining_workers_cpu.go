//go:build !cuda && !opencl

package chain

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
)

func (m *Miner) startMiningWorkers(ctx context.Context, next *atomic.Uint64, wg *sync.WaitGroup, workers int) {
	pol := m.miningPolicy()
	profile := strings.ToLower(strings.TrimSpace(pol.Profile))
	if profile == "gpu" {
		m.appendLine("miner: GPU profile requires CUDA/OpenCL build (go build -tags \"cuda,opencl\"); no GPU backend in this binary")
		return
	}
	cpuN := workers
	if pol.CPUEnabled != nil && !pol.CPUEnabled() {
		cpuN = 0
	}
	if cpuN <= 0 {
		m.appendLine("miner: CPU disabled by profile/device settings")
		return
	}
	m.poHBackend.Store("cpu")
	for w := 0; w < cpuN; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.workerLoop(ctx, next)
		}()
	}
}
