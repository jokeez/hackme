package main

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"hackme/internal/gpuhost"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// MetricsSnapshot is JSON-serialized for the dashboard API.
type MetricsSnapshot struct {
	TS int64 `json:"ts"`

	CPUPct        float64 `json:"cpu_pct"`
	MemPct        float64 `json:"mem_pct"`
	MemUsedGB     float64 `json:"mem_used_gb"`
	MemTotalGB    float64 `json:"mem_total_gb"`
	DiskUsedPct   float64 `json:"disk_used_pct"`
	DiskReadMBps  float64 `json:"disk_read_mbps"`
	DiskWriteMBps float64 `json:"disk_write_mbps"`
	NetSentMBps   float64 `json:"net_sent_mbps"`
	NetRecvMBps   float64 `json:"net_recv_mbps"`

	GPUUtilPct float64 `json:"gpu_util_pct"`
	GPUMemPct  float64 `json:"gpu_mem_pct"`
	GPUName    string  `json:"gpu_name"`
	GPUTempC   float64 `json:"gpu_temp_c"` // -1 if unavailable

	CPUTempC   float64 `json:"cpu_temp_c"`      // best-effort from host sensors; -1 if N/A
	LoadAvg1   float64 `json:"load_avg_1"`      // Unix-style 1m load; -1 on Windows / unavailable
	LoadPerCPU float64 `json:"load_per_cpu"`    // load1 / NumCPU when load valid, else -1
	MiningLoad float64 `json:"mining_load_pct"` // filled in handleMetrics: CPU-heavy when PoH running

	Goroutines int     `json:"goroutines"`
	UptimeSec  float64 `json:"uptime_sec"`
	CPUModel   string  `json:"cpu_model"`

	// Synthetic chain telemetry when miner idle (visual baseline).
	HashrateTHs   float64 `json:"hashrate_th_s"`
	ChallengeLoad float64 `json:"challenge_load"`
	Peers         int     `json:"peers"`
	BlockHeight   uint64  `json:"block_height"`

	// Economics (meta counters in chain service).
	EconMaxSupplyHMC  float64 `json:"econ_max_supply_hmc"`
	EconMintedHMC     float64 `json:"econ_total_minted_hmc"`
	EconBurnedHMC     float64 `json:"econ_total_burned_hmc"`
	EconCirculating   float64 `json:"econ_circulating_hmc"`
	EconMintRemaining float64 `json:"econ_mint_remaining_hmc"`
	EconOrderBurnRate float64 `json:"econ_order_burn_rate"`
	EconBurnImpactPct float64 `json:"econ_burn_impact_pct"`
	// Base mining reward schedule (public economics formula).
	EconBaseRewardNowHMC      float64 `json:"econ_base_reward_now_hmc"`
	EconRewardTailFloorHMC    float64 `json:"econ_reward_tail_floor_hmc"`
	EconRewardHalvingInterval uint64  `json:"econ_reward_halving_interval_blocks"`
	EconNextHalvingBlock      uint64  `json:"econ_next_halving_block"`
	EconExpectedEmptyHmcHour  float64 `json:"econ_expected_empty_hmc_hour"`
	// Recent reward window breakdown (actual observed in recent blocks).
	EconWindowSec         int64   `json:"econ_window_sec"`
	EconWindowBlocks      int     `json:"econ_window_blocks"`
	EconWindowBaseBlocks  int     `json:"econ_window_base_blocks"`
	EconWindowOrderBlocks int     `json:"econ_window_order_blocks"`
	EconWindowBaseHMC     float64 `json:"econ_window_base_hmc"`
	EconWindowOrderHMC    float64 `json:"econ_window_order_hmc"`
	EconWindowTotalHMC    float64 `json:"econ_window_total_hmc"`
	EconWindowOrderShare  float64 `json:"econ_window_order_share_pct"`

	// Live PoH / WASM miner (filled in main.handleMetrics).
	MiningPoHBackend       string  `json:"mining_poh_backend"` // cpu | cuda | opencl | mixed
	MiningRunning          bool    `json:"mining_running"`
	MiningAttemptsTotal    uint64  `json:"mining_attempts_total"`
	MiningAttemptsPerSec   float64 `json:"mining_attempts_per_sec"`
	MiningSessionSec       float64 `json:"mining_session_sec"`
	MiningLastNonce        uint64  `json:"mining_last_nonce"`
	MiningLastEval         uint64  `json:"mining_last_eval"`
	MiningLastMod997       uint64  `json:"mining_last_mod_997"`
	MiningSessionSolves    uint64  `json:"mining_session_solves"`
	MiningTargetMod        uint64  `json:"mining_target_mod"`
	MiningTargetModCap     uint64  `json:"mining_target_mod_cap"`
	MiningTargetModAtCap   bool    `json:"mining_target_mod_at_cap"`
	MiningRewardHMC        float64 `json:"mining_reward_hmc"`
	MiningWorkers          int     `json:"mining_workers"`
	MiningThrottleTarget   float64 `json:"mining_throttle_target_pct"`
	MiningObservedBlockSec float64 `json:"mining_observed_block_sec"`
	MiningTargetBlockSec   int64   `json:"mining_target_block_sec"`

	// Heuristic mining insight (not a guarantee; retarget / races / variance).
	MiningEtaSecEst         float64 `json:"mining_eta_sec_est"`          // -1 if N/A
	MiningEtaProgress       float64 `json:"mining_eta_progress"`         // 0..~1 round progress vs M
	MiningHmcLastHourApprox float64 `json:"mining_hmc_last_hour_approx"` // PoH count × ~0.01
	MiningPohBlocksLast1h   int     `json:"mining_poh_blocks_last_1h"`
	MiningProjectedHmcHour  float64 `json:"mining_projected_hmc_hour"` // from current reward & ETA
	MiningInsightNote       string  `json:"mining_insight_note"`

	MiningTaskID           string `json:"mining_task_id,omitempty"`
	MiningTaskKind         string `json:"mining_task_kind,omitempty"`
	MiningTaskSource       string `json:"mining_task_source,omitempty"`
	MiningTaskArtifactHash string `json:"mining_task_artifact_hash,omitempty"`
	MiningTaskManifestPath string `json:"mining_task_manifest_path,omitempty"`

	// MiningRigs: local command node (when mining) + remotes from POST /api/push_work.
	MiningRigs []MiningRigMetrics `json:"mining_rigs,omitempty"`

	// Per-GPU PoH (CUDA/OpenCL fleet); temps merged from nvidia-smi when available.
	MiningGPUDevices  []MiningGPUDeviceMetrics `json:"mining_gpu_devices,omitempty"`
	MiningGPUTotalGHS float64                  `json:"mining_gpu_total_gh_s,omitempty"`
	MiningGPUCount    int                      `json:"mining_gpu_count,omitempty"`

	// Pool worker on coordinator (source of truth when local WASM PoH is idle).
	PoolWorkerHashrateGHS float64 `json:"pool_worker_hashrate_gh_s,omitempty"`
	PoolWorkerTelemetry   string  `json:"pool_worker_telemetry_source,omitempty"` // coordinator | local
}

// MiningGPUDeviceMetrics mirrors chain.GPUMiningDeviceStat for JSON in main package.
type MiningGPUDeviceMetrics struct {
	Index       int     `json:"index"`
	Backend     string  `json:"backend"`
	Label       string  `json:"label"`
	Name        string  `json:"name"`
	HashrateGHS float64 `json:"hashrate_gh_s"`
	TempC       float64 `json:"temp_c"`
}

type sample struct {
	t          time.Time
	readBytes  uint64
	writeBytes uint64
	bytesSent  uint64
	bytesRecv  uint64
}

type metricsCollector struct {
	mu       sync.Mutex
	prev     sample
	start    time.Time
	ready    bool
	gpuName  string
	lastGPU  float64
	lastGMem float64
	cpuModel string
}

var collector = &metricsCollector{start: time.Now()}

// Host telemetry (nvidia-smi, lspci, sensors) is cached so concurrent /api/metrics
// callers share one probe cycle instead of N× subprocess fanout.
const metricsHostProbeTTL = 2 * time.Second

var (
	hostProbeMu      sync.Mutex
	hostProbeExpires time.Time
	hostProbeRefresh sync.Mutex
	hostProbeGU      float64
	hostProbeGM      float64
	hostProbeGTemp   float64
	hostProbeGName   string
	hostProbeLinux   string
	hostProbeCPUTemp float64
	hostProbeLoad1   float64
	hostProbeLoadPC  float64
	hostProbeCPUModel string
)

func cachedHostProbes(now time.Time) (gu, gm, gtemp float64, gname, linuxGPU, cpuModel string, cpuTemp, load1, loadPerCPU float64) {
	hostProbeMu.Lock()
	if now.Before(hostProbeExpires) {
		gu, gm, gtemp = hostProbeGU, hostProbeGM, hostProbeGTemp
		gname, linuxGPU, cpuModel = hostProbeGName, hostProbeLinux, hostProbeCPUModel
		cpuTemp, load1, loadPerCPU = hostProbeCPUTemp, hostProbeLoad1, hostProbeLoadPC
		hostProbeMu.Unlock()
		return
	}
	hostProbeMu.Unlock()

	hostProbeRefresh.Lock()
	defer hostProbeRefresh.Unlock()

	hostProbeMu.Lock()
	defer hostProbeMu.Unlock()
	if now.Before(hostProbeExpires) {
		return hostProbeGU, hostProbeGM, hostProbeGTemp, hostProbeGName, hostProbeLinux, hostProbeCPUModel,
			hostProbeCPUTemp, hostProbeLoad1, hostProbeLoadPC
	}

	gu, gm, gname, gtemp = queryNVIDIA()
	linuxGPU = detectLinuxGPUName()
	if amd := gpuhost.ListAMDGPUTelemetry(); len(amd) > 0 {
		a0 := amd[0]
		if gtemp < 0 && a0.TempC > 0 {
			gtemp = a0.TempC
		}
		if gu < 0 && a0.BusyPct >= 0 {
			gu = a0.BusyPct
		}
		if strings.TrimSpace(gname) == "" && strings.TrimSpace(a0.Name) != "" {
			gname = strings.TrimSpace(a0.Name)
		}
	}
	cpuTemp = hostCPUTempCBounded(800 * time.Millisecond)
	load1, loadPerCPU = hostLoadStats()
	cpuModel = detectCPUModel()

	hostProbeGU, hostProbeGM, hostProbeGTemp = gu, gm, gtemp
	hostProbeGName, hostProbeLinux, hostProbeCPUModel = gname, linuxGPU, cpuModel
	hostProbeCPUTemp, hostProbeLoad1, hostProbeLoadPC = cpuTemp, load1, loadPerCPU
	hostProbeExpires = now.Add(metricsHostProbeTTL)
	return
}

func (m *metricsCollector) snapshot() MetricsSnapshot {
	now := time.Now()
	// Host probes run outside the collector mutex so a slow sensor/nvidia path cannot
	// serialize every /api/metrics caller (and compound with canonical self-HTTP overlays).
	cpuPct, _ := cpu.Percent(50*time.Millisecond, false)
	var cpuVal float64
	if len(cpuPct) > 0 {
		cpuVal = cpuPct[0]
		if cpuVal < 0 {
			cpuVal = 0
		}
		if cpuVal > 100 {
			cpuVal = 100
		}
	}

	vm, err := mem.VirtualMemory()
	memPct, usedGB, totalGB := 0.0, 0.0, 0.0
	if err == nil && vm != nil {
		memPct = vm.UsedPercent
		usedGB = float64(vm.Used) / (1024 * 1024 * 1024)
		totalGB = float64(vm.Total) / (1024 * 1024 * 1024)
	}

	du, err := disk.Usage(".")
	diskUsed := 0.0
	if err == nil && du != nil {
		diskUsed = du.UsedPercent
	}

	ioCounters, _ := disk.IOCounters()
	var readB, writeB uint64
	for _, c := range ioCounters {
		readB += c.ReadBytes
		writeB += c.WriteBytes
	}

	netIO, _ := net.IOCounters(false)
	var sent, recv uint64
	if len(netIO) > 0 {
		sent = netIO[0].BytesSent
		recv = netIO[0].BytesRecv
	}

	gu, gm, gtemp, gname, linuxGPU, cpuModelProbe, cpuTempC, load1, loadPerCPU := cachedHostProbes(now)

	m.mu.Lock()
	defer m.mu.Unlock()

	dt := now.Sub(m.prev.t).Seconds()
	var readMBps, writeMBps, sentMBps, recvMBps float64
	if m.ready && dt > 0.05 {
		readMBps = float64(readB-m.prev.readBytes) / (1024 * 1024) / dt
		writeMBps = float64(writeB-m.prev.writeBytes) / (1024 * 1024) / dt
		sentMBps = float64(sent-m.prev.bytesSent) / (1024 * 1024) / dt
		recvMBps = float64(recv-m.prev.bytesRecv) / (1024 * 1024) / dt
		if readMBps < 0 {
			readMBps = 0
		}
		if writeMBps < 0 {
			writeMBps = 0
		}
		if sentMBps < 0 {
			sentMBps = 0
		}
		if recvMBps < 0 {
			recvMBps = 0
		}
	}
	m.prev = sample{t: now, readBytes: readB, writeBytes: writeB, bytesSent: sent, bytesRecv: recv}
	m.ready = true

	if gname != "" {
		m.gpuName = gname
	}
	if m.gpuName == "" && linuxGPU != "" {
		m.gpuName = linuxGPU
	}
	if gu >= 0 {
		m.lastGPU = gu
	}
	if gm >= 0 {
		m.lastGMem = gm
	}
	gpuName := m.gpuName
	if gpuName == "" {
		gpuName = "—"
	}
	if m.cpuModel == "" {
		m.cpuModel = cpuModelProbe
	}

	return MetricsSnapshot{
		TS:            now.UnixMilli(),
		CPUPct:        round2(cpuVal),
		MemPct:        round2(memPct),
		MemUsedGB:     round2(usedGB),
		MemTotalGB:    round2(totalGB),
		DiskUsedPct:   round2(diskUsed),
		DiskReadMBps:  round2(readMBps),
		DiskWriteMBps: round2(writeMBps),
		NetSentMBps:   round2(sentMBps),
		NetRecvMBps:   round2(recvMBps),
		GPUUtilPct:    round2(m.lastGPU),
		GPUMemPct:     round2(m.lastGMem),
		GPUName:       gpuName,
		GPUTempC:      round2(gtemp),
		CPUTempC:      round2(cpuTempC),
		LoadAvg1:      round2(load1),
		LoadPerCPU:    round2(loadPerCPU),
		MiningLoad:    -1,
		Goroutines:    runtime.NumGoroutine(),
		UptimeSec:     round2(now.Sub(m.start).Seconds()),
		CPUModel:      m.cpuModel,
		HashrateTHs:   0,
		ChallengeLoad: 0,
		Peers:         0,
		BlockHeight:   0,
	}
}

func detectLinuxGPUName() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "lspci").Output()
	if err != nil || len(out) == 0 {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, ln := range lines {
		l := strings.TrimSpace(ln)
		if l == "" {
			continue
		}
		low := strings.ToLower(l)
		if !strings.Contains(low, "vga compatible controller") && !strings.Contains(low, "3d controller") {
			continue
		}
		// Format: "01:00.0 VGA compatible controller: Vendor Model (rev ..)"
		parts := strings.SplitN(l, ":", 3)
		if len(parts) < 3 {
			continue
		}
		model := strings.TrimSpace(parts[2])
		// Drop bracket suffixes like "[...]" for cleaner UI.
		for {
			i := strings.Index(model, "[")
			j := strings.Index(model, "]")
			if i >= 0 && j > i {
				model = strings.TrimSpace(model[:i] + model[j+1:])
				continue
			}
			break
		}
		model = strings.Join(strings.Fields(model), " ")
		if model != "" {
			return model
		}
	}
	return ""
}

func detectCPUModel() string {
	info, err := cpu.Info()
	if err != nil || len(info) == 0 {
		return runtime.GOARCH
	}
	model := strings.TrimSpace(info[0].ModelName)
	if model == "" {
		return runtime.GOARCH
	}
	return model
}

func hostCPUTempC() float64 {
	return hostCPUTempCBounded(0)
}

// hostCPUTempCBounded samples sensors with an optional wall deadline so /api/metrics cannot stall.
func hostCPUTempCBounded(maxWait time.Duration) float64 {
	if maxWait <= 0 {
		return hostCPUTempCUnbounded()
	}
	ch := make(chan float64, 1)
	go func() { ch <- hostCPUTempCUnbounded() }()
	select {
	case v := <-ch:
		return v
	case <-time.After(maxWait):
		return -1
	}
}

func hostCPUTempCUnbounded() float64 {
	sensors, err := host.SensorsTemperatures()
	if err != nil || len(sensors) == 0 {
		return -1
	}
	var sum float64
	var n int
	for _, s := range sensors {
		if s.Temperature <= 0 || s.Temperature > 150 {
			continue
		}
		key := strings.ToLower(s.SensorKey)
		if strings.Contains(key, "cpu") || strings.Contains(key, "core") || strings.Contains(key, "package") || strings.Contains(key, "acpi") {
			sum += s.Temperature
			n++
		}
	}
	if n == 0 {
		// fallback: average all plausible readings
		for _, s := range sensors {
			if s.Temperature > 0 && s.Temperature < 120 {
				sum += s.Temperature
				n++
			}
		}
	}
	if n == 0 {
		return -1
	}
	return sum / float64(n)
}

func hostLoadStats() (load1 float64, perCPU float64) {
	load1, perCPU = -1, -1
	avg, err := load.Avg()
	if err != nil || avg == nil {
		return load1, perCPU
	}
	load1 = avg.Load1
	n := runtime.NumCPU()
	if n > 0 {
		perCPU = avg.Load1 / float64(n)
	}
	return load1, perCPU
}

func round2(v float64) float64 {
	return math.Round(v*100) / 100
}

// queryNVIDIA parses nvidia-smi when available (Windows/Linux).
// Returns util%, mem%, name, temp°C; util -1 if unavailable.
// nvidiaGPURow is one line from nvidia-smi multi-GPU query.
type nvidiaGPURow struct {
	Index int
	Util  float64
	TempC float64
	Name  string
}

const nvidiaSMITimeout = 2500 * time.Millisecond

func nvidiaSMIRun(args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), nvidiaSMITimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "nvidia-smi", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// queryNVIDIAMulti returns one row per physical NVIDIA GPU (index matches CUDA ordinal).
func queryNVIDIAMulti() []nvidiaGPURow {
	rows := queryNVIDIAMultiSMI()
	if len(rows) > 0 {
		return rows
	}
	return queryNVIDIAMultiProc()
}

func queryNVIDIAMultiSMI() []nvidiaGPURow {
	out, err := nvidiaSMIRun(
		"--query-gpu=index,temperature.gpu,utilization.gpu,name",
		"--format=csv,noheader,nounits")
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	var rows []nvidiaGPURow
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 4 {
			continue
		}
		idx, err0 := strconv.Atoi(strings.TrimSpace(parts[0]))
		t, err1 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
		u, err2 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
		name := strings.TrimSpace(strings.Join(parts[3:], ","))
		if err0 != nil || err1 != nil || err2 != nil {
			continue
		}
		rows = append(rows, nvidiaGPURow{Index: idx, TempC: t, Util: u, Name: name})
	}
	return rows
}

func queryNVIDIAMultiProc() []nvidiaGPURow {
	proc := gpuhost.ListNVIDIAProcCards()
	if len(proc) == 0 {
		return nil
	}
	rows := make([]nvidiaGPURow, 0, len(proc))
	for _, c := range proc {
		rows = append(rows, nvidiaGPURow{
			Index: c.Index,
			Name:  c.Name,
			Util:  -1,
			TempC: -1,
		})
	}
	return rows
}

func queryNVIDIA() (util float64, memPct float64, name string, tempC float64) {
	util, memPct, tempC = -1, -1, -1
	if rows := queryNVIDIAMulti(); len(rows) > 0 {
		r := rows[0]
		return r.Util, memPct, r.Name, r.TempC
	}
	out, err := nvidiaSMIRun(
		"--query-gpu=utilization.gpu,memory.used,memory.total,temperature.gpu,name",
		"--format=csv,noheader,nounits")
	if err != nil {
		if proc := queryNVIDIAMultiProc(); len(proc) > 0 {
			return -1, -1, proc[0].Name, -1
		}
		return -1, -1, "", -1
	}
	line := strings.TrimSpace(string(out))
	if line == "" {
		return -1, -1, "", -1
	}
	first := strings.Split(line, "\n")[0]
	parts := strings.Split(first, ",")
	if len(parts) < 5 {
		return -1, -1, "", -1
	}
	u, err1 := strconv.ParseFloat(strings.TrimSpace(parts[0]), 64)
	mu, err2 := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
	mt, err3 := strconv.ParseFloat(strings.TrimSpace(parts[2]), 64)
	t, err4 := strconv.ParseFloat(strings.TrimSpace(parts[3]), 64)
	name = strings.TrimSpace(strings.Join(parts[4:], ","))
	if err1 != nil {
		u = -1
	}
	if err2 != nil || err3 != nil || mt <= 0 {
		memPct = -1
	} else {
		memPct = (mu / mt) * 100
	}
	if err4 == nil && t > 0 {
		tempC = t
	}
	return u, memPct, name, tempC
}

// queryAMDGPUMulti returns amdgpu DRM cards in sysfs (Linux), same shape as nvidia rows for merge helpers.
func queryAMDGPUMulti() []nvidiaGPURow {
	var rows []nvidiaGPURow
	for _, a := range gpuhost.ListAMDGPUTelemetry() {
		rows = append(rows, nvidiaGPURow{
			Index: a.Index, Util: a.BusyPct, TempC: a.TempC, Name: a.Name,
		})
	}
	return rows
}

func writeMetricsJSON(w http.ResponseWriter, s MetricsSnapshot) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(s)
}
