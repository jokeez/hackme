package chain

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"hackme/internal/gpuhost"
	"hackme/internal/sandbox"
)

// Hot-path batch sizes (tuned for throughput + UI stride).
const (
	workerInnerLoop = 100_000 // tight inner iterations per claimed range
	statStride      = 50_000  // update last nonce/eval + partial attempts cadence
	logInterval     = 2 * time.Second
)

// MiningStats is live telemetry for the dashboard.
type MiningStats struct {
	Running        bool    `json:"running"`
	PoHBackend     string  `json:"mining_poh_backend"` // "cpu" | "cuda" | "opencl" | "mixed" (GPU builds)
	AttemptsTotal  uint64  `json:"attempts_total"`
	AttemptsPerSec float64 `json:"attempts_per_sec"`
	SessionSeconds float64 `json:"session_seconds"`
	LastNonce      uint64  `json:"last_nonce"`
	LastEval       uint64  `json:"last_eval"`
	LastEvalMod    uint64  `json:"last_eval_mod"`
	TargetMod      uint64  `json:"target_mod"`
	RewardHMC      float64 `json:"reward_hmc"`
	SessionSolves  uint64  `json:"session_solves"`
	WASMFunction   string  `json:"wasm_function"`
	Workers        int     `json:"workers"`
	ThrottleCPUPct float64 `json:"throttle_target_cpu_pct"` // host CPU % target; workers sleep briefly when sampling exceeds it

	TaskID           string `json:"task_id,omitempty"`
	TaskKind         string `json:"task_kind,omitempty"`
	TaskSource       string `json:"task_source,omitempty"`
	TaskArtifactHash string `json:"task_artifact_hash,omitempty"`
	TaskManifestPath string `json:"task_manifest_path,omitempty"`

	// GPUPoHDevices is filled when mining on GPU(s); each entry is one physical accelerator.
	GPUPoHDevices []GPUMiningDeviceStat `json:"mining_gpu_devices,omitempty"`
}

// miningLogChanBuffer is per-SSE subscriber; lines drop if the client is slow.
const miningLogChanBuffer = 64

// Miner runs parallel native PoH search; WASM verifies the winning nonce only.
type Miner struct {
	mu       sync.Mutex
	lines    []string
	logSubs  map[chan string]struct{} // broadcast new log lines (SSE)
	maxLines int
	cancel   context.CancelFunc
	running  atomic.Bool
	onSolve  func(ctx context.Context, nonce, v, targetMod uint64) error
	loadMod  func(context.Context) (uint64, error)
	tasks    TaskProvider

	rewardHMC float64

	currentMod     atomic.Uint64
	taskSnap       atomic.Value // TaskSpec
	attempts       atomic.Uint64
	lastNonce      atomic.Uint64
	lastEval       atomic.Uint64
	sessionStartNs atomic.Int64
	sessionSolves  atomic.Uint64

	solved      atomic.Uint32 // 0 = hunting, 1 = winner claimed
	throttleMu  sync.RWMutex
	throttlePct float64 // target max CPU % for process (soft hint via sleep); see SetSoftCPUThrottlePct

	// poHBackend holds "cpu", "cuda", "opencl", or "mixed" for metrics.
	poHBackend atomic.Value

	gpuFleetMu     sync.RWMutex
	gpuFleetN      int
	gpuFleetLabels []string
	gpuFleetNames  []string
	gpuFleetBack   []string
	gpuDevAttempts [16]atomic.Uint64

	policyMu     sync.RWMutex
	devicePolicy MiningDevicePolicy
}

func NewMiner(rewardHMC float64, loadMod func(context.Context) (uint64, error), onSolve func(ctx context.Context, nonce, v, targetMod uint64) error, tasks TaskProvider) *Miner {
	if rewardHMC <= 0 {
		rewardHMC = 0.01
	}
	if loadMod == nil {
		loadMod = func(context.Context) (uint64, error) { return DefaultPoHTargetMod, nil }
	}
	if tasks == nil {
		tasks = InternalTaskProvider{}
	}
	m := &Miner{
		maxLines:    200,
		loadMod:     loadMod,
		onSolve:     onSolve,
		tasks:       tasks,
		rewardHMC:   rewardHMC,
		throttlePct: DefaultSoftCPUThrottlePct,
	}
	if ev := strings.TrimSpace(os.Getenv("HACKME_MINER_CPU_PCT")); ev != "" {
		if x, err := strconv.ParseFloat(ev, 64); err == nil && x > 0 && x <= 100 {
			m.throttlePct = x
		}
	}
	s0, _ := tasks.Snapshot(context.Background())
	m.taskSnap.Store(s0)
	return m
}

// DefaultSoftCPUThrottlePct is the initial soft CPU load target before env/API overrides.
const DefaultSoftCPUThrottlePct = 80

// SetSoftCPUThrottlePct sets the soft host CPU cap for PoH workers (1–100).
// Values ≥99.5 effectively disable the extra sleep path in maybeSleepIfHostCPUHigh.
func (m *Miner) SetSoftCPUThrottlePct(pct float64) error {
	if m == nil {
		return fmt.Errorf("miner: nil")
	}
	if pct < 1 || pct > 100 || math.IsNaN(pct) {
		return fmt.Errorf("miner: soft_cap_pct must be 1..100")
	}
	m.throttleMu.Lock()
	m.throttlePct = pct
	m.throttleMu.Unlock()
	return nil
}

func (m *Miner) softThrottlePct() float64 {
	m.throttleMu.RLock()
	v := m.throttlePct
	m.throttleMu.RUnlock()
	return v
}

// Stats returns counters for the current or last mining session.
func (m *Miner) Stats() MiningStats {
	mod := m.currentMod.Load()
	if mod == 0 {
		mod = DefaultPoHTargetMod
	}
	backend := "cpu"
	if v := m.poHBackend.Load(); v != nil {
		if s, ok := v.(string); ok && s != "" {
			backend = s
		}
	}
	st := MiningStats{
		Running:        m.running.Load(),
		PoHBackend:     backend,
		TargetMod:      mod,
		RewardHMC:      m.RewardForSolve(),
		WASMFunction:   "native n*7+13 · WASM verify on solve",
		Workers:        runtime.NumCPU(),
		ThrottleCPUPct: m.softThrottlePct(),
	}
	st.AttemptsTotal = m.attempts.Load()
	st.LastNonce = m.lastNonce.Load()
	st.LastEval = m.lastEval.Load()
	if st.LastEval > 0 || st.AttemptsTotal > 0 {
		st.LastEvalMod = st.LastEval % mod
	}
	startNs := m.sessionStartNs.Load()
	if startNs > 0 {
		elapsed := float64(time.Now().UnixNano()-startNs) / 1e9
		if elapsed < 0 {
			elapsed = 0
		}
		st.SessionSeconds = elapsed
		if elapsed >= 0.001 && st.AttemptsTotal > 0 {
			st.AttemptsPerSec = float64(st.AttemptsTotal) / elapsed
		}
	}
	st.SessionSolves = m.sessionSolves.Load()
	if st.AttemptsPerSec > 0 {
		st.AttemptsPerSec = math.Round(st.AttemptsPerSec*100) / 100
	}
	st.SessionSeconds = math.Round(st.SessionSeconds*100) / 100

	m.gpuFleetMu.RLock()
	nGPU := m.gpuFleetN
	labels := append([]string(nil), m.gpuFleetLabels...)
	names := append([]string(nil), m.gpuFleetNames...)
	backs := append([]string(nil), m.gpuFleetBack...)
	m.gpuFleetMu.RUnlock()
	if nGPU > 0 && st.SessionSeconds >= 0.001 {
		st.Workers = nGPU
		temps := gpuhost.PoHGPUTemps()
		for i := 0; i < nGPU && i < len(labels); i++ {
			att := m.gpuDevAttempts[i].Load()
			gh := float64(att) / st.SessionSeconds / 1e6
			if gh < 0 {
				gh = 0
			}
			name := ""
			if i < len(names) {
				name = names[i]
			}
			be := ""
			if i < len(backs) {
				be = backs[i]
			}
			lb := ""
			if i < len(labels) {
				lb = labels[i]
			}
			tempC := -1.0
			if temps != nil {
				if t, ok := temps[i]; ok && t > 0 {
					tempC = t
				}
			}
			st.GPUPoHDevices = append(st.GPUPoHDevices, GPUMiningDeviceStat{
				Index:       i,
				Backend:     be,
				Label:       lb,
				Name:        name,
				HashrateGHS: math.Round(gh*100) / 100,
				TempC:       tempC,
			})
		}
	}

	ts := m.TaskSnapshot()
	st.TaskID = ts.ID
	st.TaskKind = string(ts.Kind)
	st.TaskSource = ts.Source
	st.TaskArtifactHash = ts.ArtifactHash
	st.TaskManifestPath = ts.ManifestPath
	if len(ts.WasmCheck) > 0 {
		st.WASMFunction = "native PoH + wasm gate check(n); WASM eval verify on solve"
	}
	return st
}

// TaskSnapshot returns the last refreshed task manifest (internal default if unset).
func (m *Miner) TaskSnapshot() TaskSpec {
	v := m.taskSnap.Load()
	if v == nil {
		s, _ := InternalTaskProvider{}.Snapshot(context.Background())
		return s
	}
	ts, ok := v.(TaskSpec)
	if !ok {
		s, _ := InternalTaskProvider{}.Snapshot(context.Background())
		return s
	}
	return ts
}

// RewardForSolve returns manifest reward if set, otherwise the miner default.
func (m *Miner) RewardForSolve() float64 {
	s := m.TaskSnapshot()
	if s.RewardHMC > 0 {
		return s.RewardHMC
	}
	return m.rewardHMC
}

func (m *Miner) clearGPUPoHFleet() {
	m.gpuFleetMu.Lock()
	defer m.gpuFleetMu.Unlock()
	m.gpuFleetN = 0
	m.gpuFleetLabels = nil
	m.gpuFleetNames = nil
	m.gpuFleetBack = nil
	for i := range m.gpuDevAttempts {
		m.gpuDevAttempts[i].Store(0)
	}
}

func (m *Miner) setGPUPoHFleet(labels, names, backs []string, backendSummary string) {
	m.gpuFleetMu.Lock()
	defer m.gpuFleetMu.Unlock()
	n := len(labels)
	if len(names) < n {
		n = len(names)
	}
	if len(backs) < n {
		n = len(backs)
	}
	if n > len(m.gpuDevAttempts) {
		n = len(m.gpuDevAttempts)
	}
	m.gpuFleetLabels = append([]string(nil), labels[:n]...)
	m.gpuFleetNames = append([]string(nil), names[:n]...)
	m.gpuFleetBack = append([]string(nil), backs[:n]...)
	m.gpuFleetN = n
	m.poHBackend.Store(backendSummary)
}

func (m *Miner) appendLine(s string) {
	m.mu.Lock()
	m.lines = append(m.lines, s)
	if len(m.lines) > m.maxLines {
		m.lines = m.lines[len(m.lines)-m.maxLines:]
	}
	subs := make([]chan string, 0, len(m.logSubs))
	for ch := range m.logSubs {
		subs = append(subs, ch)
	}
	m.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- s:
		default:
		}
	}
}

// SubscribeLogLines delivers each new appendLine message until ctx is cancelled.
// Snapshot past lines with Logs(); do not call from the same goroutine that blocks on the channel.
func (m *Miner) SubscribeLogLines(ctx context.Context) <-chan string {
	ch := make(chan string, miningLogChanBuffer)
	m.mu.Lock()
	if m.logSubs == nil {
		m.logSubs = make(map[chan string]struct{})
	}
	m.logSubs[ch] = struct{}{}
	m.mu.Unlock()
	go func() {
		<-ctx.Done()
		m.mu.Lock()
		delete(m.logSubs, ch)
		m.mu.Unlock()
		close(ch)
	}()
	return ch
}

// Logs returns a copy of recent log lines.
func (m *Miner) Logs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.lines))
	copy(out, m.lines)
	return out
}

// Running reports whether search is active.
func (m *Miner) Running() bool {
	return m.running.Load()
}

// SetRunningForTest pins the running flag for unit tests (avoids full PoH batch completing in <1s).
func (m *Miner) SetRunningForTest(on bool) {
	if m == nil {
		return
	}
	m.running.Store(on)
}

// Stop cancels background search.
func (m *Miner) Stop() {
	if !m.running.Load() {
		return
	}
	if m.cancel != nil {
		m.cancel()
	}
}

func (m *Miner) runLogTicker(ctx context.Context) {
	t := time.NewTicker(logInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Keep target mod fresh even without solved blocks so difficulty can
			// recover from stale/high values after hashrate shifts.
			m.refreshTargetMod(ctx)
			m.updateTaskSnap(ctx)
			att := m.attempts.Load()
			start := m.sessionStartNs.Load()
			elapsed := float64(time.Now().UnixNano()-start) / 1e9
			var hps float64
			if elapsed > 0.01 {
				hps = float64(att) / elapsed
			}
			line := fmt.Sprintf("miner: ~%.0f H/s · attempts=%d · last_nonce=%d · workers=%d",
				hps, att, m.lastNonce.Load(), runtime.NumCPU())
			m.appendLine(line)
			log.Printf("hackme: %s", line)
		}
	}
}

// trySolveCommit claims the win (CAS), verifies with WASM once, runs onSolve (writes block + reward).
// Resets per-round counters and returns true so the worker starts a new batch; returns false to keep scanning.
func (m *Miner) trySolveCommit(ctx context.Context, n, vNative, targetMod uint64) bool {
	if !m.solved.CompareAndSwap(0, 1) {
		return false // another worker is committing; try next nonce
	}
	vw, err := sandbox.Eval(ctx, n)
	if err != nil || vw != vNative {
		m.solved.Store(0)
		if err != nil {
			m.appendLine("miner: WASM verify err · " + err.Error())
		} else {
			m.appendLine("miner: WASM verify mismatch (value)")
		}
		return false
	}
	m.lastNonce.Store(n)
	m.lastEval.Store(vw)
	m.appendLine(fmt.Sprintf("miner: SOLVED nonce=%d eval=%d (mod %d == 0)", n, vw, targetMod))
	if m.onSolve != nil {
		if err := m.onSolve(ctx, n, vw, targetMod); err != nil {
			m.appendLine("miner: onSolve err: " + err.Error())
			m.solved.Store(0)
			m.refreshTargetMod(ctx)
			return false
		}
		m.sessionSolves.Add(1)
	}
	m.refreshTargetMod(ctx)
	m.updateTaskSnap(ctx)
	// Fresh round for H/s and attempts UI; chain tip already updated inside onSolve.
	m.attempts.Store(0)
	m.sessionStartNs.Store(time.Now().UnixNano())
	m.solved.Store(0)
	m.appendLine("miner: block committed · continuing scan (Stop to halt)")
	return true
}

func (m *Miner) workerLoop(ctx context.Context, next *atomic.Uint64) {
	var batchN uint64
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		base := next.Add(workerInnerLoop) - workerInnerLoop

		var mod uint64
		for i := uint64(0); i < workerInnerLoop; i++ {
			if i%statStride == 0 {
				mod = m.currentMod.Load()
				if mod < pohRetargetMinMod {
					mod = DefaultPoHTargetMod
				}
			}
			n := base + i
			v := PohEval(n)

			if (i+1)%statStride == 0 {
				m.lastNonce.Store(n)
				m.lastEval.Store(v)
				m.attempts.Add(statStride)
			}

			if mod > 0 && v%mod == 0 {
				ts := m.TaskSnapshot()
				if len(ts.WasmCheck) > 0 {
					ok, err := sandbox.InvokeCheck(ctx, ts.WasmCheck, n)
					if err != nil {
						m.appendLine("miner: wasm gate: " + err.Error())
					}
					if err != nil || !ok {
						rem := (i + 1) % statStride
						if rem != 0 {
							m.attempts.Add(rem)
						}
						continue
					}
				}
				rem := (i + 1) % statStride
				if rem != 0 {
					m.attempts.Add(rem)
				}
				if m.trySolveCommit(ctx, n, v, mod) {
					break // new round: claim next batch, prev_hash already on disk
				}
			}
		}
		batchN++
		m.maybeSleepIfHostCPUHigh(batchN)
	}
}

// Start launches runtime.NumCPU() workers until Stop (runs continuous PoH rounds after each block).
func (m *Miner) Start(ctx context.Context) {
	if !m.running.CompareAndSwap(false, true) {
		m.appendLine("miner: already running")
		return
	}
	m.mu.Lock()
	m.lines = nil
	m.mu.Unlock()
	m.clearGPUPoHFleet()
	m.attempts.Store(0)
	m.sessionSolves.Store(0)
	m.solved.Store(0)
	m.sessionStartNs.Store(time.Now().UnixNano())

	mod := DefaultPoHTargetMod
	if m.loadMod != nil {
		if v, err := m.loadMod(ctx); err == nil {
			mod = ClampPoHTargetMod(v)
		} else {
			m.appendLine("miner: PoHTargetMod load err · " + err.Error())
		}
	}
	m.currentMod.Store(mod)
	m.appendLine(fmt.Sprintf("miner: target mod %d (retarget every %d blocks · target ~%ds/block)",
		mod, PoHRetargetWindowBlocks, PoHRetargetTargetSec))
	m.updateTaskSnap(ctx)

	ctx, m.cancel = context.WithCancel(ctx)

	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}

	var nextNonce atomic.Uint64
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err == nil {
		// Random aligned start so each session doesn't always hit the same low nonces first.
		start := binary.BigEndian.Uint64(rnd[:]) % uint64(1<<50)
		start = (start / workerInnerLoop) * workerInnerLoop
		nextNonce.Store(start)
	}

	m.appendLine(fmt.Sprintf("miner: %d workers · batch %d nonces (stride %d) · PoH scan · logs every %v",
		workers, workerInnerLoop, statStride, logInterval))
	log.Printf("hackme: miner start workers=%d batch=%d", workers, workerInnerLoop)

	go func() {
		defer m.running.Store(false)
		defer func() { m.cancel = nil }()

		tickCtx, tickCancel := context.WithCancel(ctx)
		go m.runLogTicker(tickCtx)

		var wg sync.WaitGroup
		m.startMiningWorkers(ctx, &nextNonce, &wg, workers)
		wg.Wait()
		tickCancel()
		m.appendLine("miner: stopped")
		log.Printf("hackme: miner stopped attempts=%d", m.attempts.Load())
	}()
}

func (m *Miner) refreshTargetMod(ctx context.Context) {
	if m.loadMod == nil {
		return
	}
	mod, err := m.loadMod(ctx)
	if err != nil {
		return
	}
	m.currentMod.Store(ClampPoHTargetMod(mod))
}

func (m *Miner) updateTaskSnap(ctx context.Context) {
	if m.tasks == nil {
		return
	}
	s, err := m.tasks.Snapshot(ctx)
	if err != nil {
		return
	}
	prev := m.TaskSnapshot()
	m.taskSnap.Store(s)
	if prev.ID != s.ID || prev.Source != s.Source || prev.ManifestPath != s.ManifestPath {
		m.appendLine(fmt.Sprintf("miner: active task %q (%s) kind=%s", s.ID, s.Source, s.Kind))
	}
}
