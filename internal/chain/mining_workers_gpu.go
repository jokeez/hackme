//go:build cuda || opencl

package chain

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hackme/internal/gpuhost"
	"hackme/internal/gpupoh"
	"hackme/internal/sandbox"
)

func useGPUPoH() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_USE_CUDA"))
	if v == "0" || strings.EqualFold(v, "false") || strings.EqualFold(v, "no") {
		return false
	}
	return true
}

func gpuBackendSummary(accs []gpupoh.Accelerator) string {
	var cudaN, oclN int
	for _, a := range accs {
		switch a.Backend() {
		case "cuda":
			cudaN++
		case "opencl":
			oclN++
		}
	}
	if cudaN > 0 && oclN > 0 {
		return "mixed"
	}
	if cudaN > 0 {
		return "cuda"
	}
	return "opencl"
}

func (m *Miner) startMiningWorkers(ctx context.Context, next *atomic.Uint64, wg *sync.WaitGroup, workers int) {
	pol := m.miningPolicy()
	profile := strings.ToLower(strings.TrimSpace(pol.Profile))
	if profile == "" {
		profile = "mixed"
	}

	startCPU := func(n int) {
		if n <= 0 {
			return
		}
		m.poHBackend.Store("cpu")
		for w := 0; w < n; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				m.workerLoop(ctx, next)
			}()
		}
	}

	if profile == "cpu" || !useGPUPoH() {
		cpuN := workers
		if pol.CPUEnabled != nil && !pol.CPUEnabled() {
			cpuN = 0
		}
		if cpuN == 0 {
			m.appendLine("miner: CPU disabled by profile/device settings")
		}
		startCPU(cpuN)
		return
	}

	var accs []gpupoh.Accelerator
	var err error
	if profile != "cpu" {
		accs, err = gpupoh.DiscoverAccelerators()
		if err == nil && len(accs) > 0 {
			filtered := make([]gpupoh.Accelerator, 0, len(accs))
			for _, a := range accs {
				if pol.GPUEnabled != nil && !pol.GPUEnabled(a.Backend(), a.DeviceIndex()) {
					_ = a.Close()
					continue
				}
				filtered = append(filtered, a)
			}
			accs = filtered
		}
	}

	if len(accs) > 0 {
		if len(accs) > gpupoh.MaxGPUDevices {
			for i := gpupoh.MaxGPUDevices; i < len(accs); i++ {
				_ = accs[i].Close()
			}
			accs = accs[:gpupoh.MaxGPUDevices]
			m.appendLine(fmt.Sprintf("miner: GPU fleet capped at %d devices", gpupoh.MaxGPUDevices))
		}
		order := sortAcceleratorsByPriority(accs, pol.GPUPriority)
		labels := make([]string, len(accs))
		names := make([]string, len(accs))
		backs := make([]string, len(accs))
		for i, a := range accs {
			labels[i] = a.Label()
			names[i] = a.DeviceName()
			backs[i] = a.Backend()
		}
		m.setGPUPoHFleet(labels, names, backs, gpuBackendSummary(accs))
		m.appendLine("miner: PoH GPU fleet · " + summarizeFleet(accs))
		log.Printf("hackme: gpu mining fleet size=%d backend=%s profile=%s", len(accs), gpuBackendSummary(accs), profile)
		for _, oi := range order {
			ai, acc := oi, accs[oi]
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() { _ = acc.Close() }()
				m.gpuSearchLoop(ctx, next, acc, ai)
			}()
		}
		if profile == "gpu" {
			return
		}
	} else if profile == "gpu" {
		if err != nil {
			m.appendLine("miner: GPU profile but no accelerators · " + err.Error() + " · CPU fallback")
			log.Printf("hackme: GPU profile unavailable: %v", err)
		} else {
			m.appendLine("miner: GPU profile but no enabled accelerators · CPU fallback")
		}
	} else if err != nil {
		m.appendLine("miner: GPU unavailable · " + err.Error() + " · CPU fallback")
		log.Printf("hackme: GPU unavailable: %v", err)
	}

	cpuN := workers
	if profile == "gpu" {
		cpuN = 0
	} else if pol.CPUEnabled != nil && !pol.CPUEnabled() {
		cpuN = 0
	}
	startCPU(cpuN)
}

func summarizeFleet(accs []gpupoh.Accelerator) string {
	var b strings.Builder
	for i, a := range accs {
		if i > 0 {
			b.WriteString(" · ")
		}
		b.WriteString(a.Label())
		b.WriteString(" ")
		b.WriteString(strings.TrimSpace(a.DeviceName()))
	}
	return b.String()
}

const gpuBatch = 1 << 22

func gpuThermalPauseResumeC() (pause, resume float64, enabled bool) {
	s := strings.TrimSpace(os.Getenv("HACKME_GPU_TEMP_PAUSE_C"))
	if s == "" {
		return 0, 0, false
	}
	p, err := strconv.ParseFloat(s, 64)
	if err != nil || p <= 0 {
		return 0, 0, false
	}
	pause = p
	rs := strings.TrimSpace(os.Getenv("HACKME_GPU_TEMP_RESUME_C"))
	if rs != "" {
		if r, err := strconv.ParseFloat(rs, 64); err == nil && r > 0 {
			resume = r
		}
	}
	if resume <= 0 {
		resume = pause - 10
		if resume < 0 {
			resume = 0
		}
	}
	return pause, resume, true
}

func (m *Miner) gpuSearchLoop(ctx context.Context, next *atomic.Uint64, acc gpupoh.Accelerator, devSlot int) {
	if devSlot < 0 || devSlot >= len(m.gpuDevAttempts) {
		devSlot = 0
	}
	pauseC, resumeC, thermalOn := gpuThermalPauseResumeC()
	var lastThermalLog time.Time
	var lastTempCheck time.Time
	const tempCheckEvery = 2 * time.Second
	var batchN uint64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if thermalOn && time.Since(lastTempCheck) >= tempCheckEvery {
			lastTempCheck = time.Now()
			temps := gpuhost.PoHGPUTemps()
			t := temps[devSlot]
			if len(temps) > 0 && t >= pauseC {
				if time.Since(lastThermalLog) > 8*time.Second {
					m.appendLine(fmt.Sprintf("miner: GPU #%d thermal hold · %.0f°C ≥ %.0f°C (HACKME_GPU_TEMP_PAUSE_C)", devSlot, t, pauseC))
					lastThermalLog = time.Now()
				}
				for {
					select {
					case <-ctx.Done():
						return
					default:
					}
					time.Sleep(2 * time.Second)
					temps = gpuhost.PoHGPUTemps()
					t = temps[devSlot]
					if len(temps) == 0 || t <= resumeC {
						m.appendLine(fmt.Sprintf("miner: GPU #%d thermal resume · %.0f°C ≤ %.0f°C", devSlot, t, resumeC))
						break
					}
				}
				continue
			}
		}
		mod := m.currentMod.Load()
		if mod < pohRetargetMinMod {
			mod = DefaultPoHTargetMod
		}
		base := next.Add(gpuBatch) - gpuBatch
		found, n, err := acc.Search(ctx, base, gpuBatch, mod)
		if err != nil {
			m.appendLine("miner: GPU batch err · " + acc.Label() + " · " + err.Error())
			return
		}
		m.attempts.Add(gpuBatch)
		m.gpuDevAttempts[devSlot].Add(gpuBatch)
		endN := base + gpuBatch - 1
		m.lastNonce.Store(endN)
		m.lastEval.Store(PohEval(endN))
		if found {
			v := PohEval(n)
			m.lastNonce.Store(n)
			m.lastEval.Store(v)
			// Chain M can change between batch start and hit (retarget / other worker);
			// AppendPoHBlock rejects target_mod != meta — use fresh M and skip stale hits.
			modNow := m.currentMod.Load()
			if modNow < pohRetargetMinMod {
				modNow = DefaultPoHTargetMod
			}
			if modNow > 0 && v%modNow == 0 {
				ts := m.TaskSnapshot()
				if len(ts.WasmCheck) > 0 {
					ok, err := sandbox.InvokeCheck(ctx, ts.WasmCheck, n)
					if err != nil || !ok {
						batchN++
						m.maybeSleepIfHostCPUHigh(batchN)
						continue
					}
				}
				if m.trySolveCommit(ctx, n, v, modNow) {
					// fall through: batchN++ below
				}
			}
		}
		batchN++
		m.maybeSleepIfHostCPUHigh(batchN)
	}
}
