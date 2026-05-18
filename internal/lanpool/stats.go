package lanpool

import (
	"fmt"
	"math/rand"
	"sort"
	"time"
)

// LocalMining is command-node PoH contribution for global TH/s (same scale as metrics).
type LocalMining struct {
	Running        bool
	AttemptsPerSec float64
	GPUTotalGHS    float64
}

func localGH(local LocalMining) float64 {
	if local.GPUTotalGHS > 0 {
		return local.GPUTotalGHS
	}
	if local.Running {
		gh := local.AttemptsPerSec / 1e6
		if gh < 0.01 {
			gh = local.AttemptsPerSec / 1e3
		}
		return gh
	}
	return 0
}

// MockNetworkStats keeps the old simulated global totals (HACKME_NETWORK_MOCK=1).
func MockNetworkStats(reg *Registry, localNodeID string) NetworkStatsResponse {
	bucket := time.Now().Unix() / 4
	r := rand.New(rand.NewSource(bucket * 7919))
	baseMiners := 900 + r.Intn(800)
	baseTH := 180.0 + r.Float64()*380.0
	peers := 3 + r.Intn(18)

	top := make([]string, 5)
	for i := range top {
		top[i] = fmt.Sprintf("HMC-%016x", r.Uint64())
	}
	if localNodeID != "" {
		top[r.Intn(5)] = localNodeID
	}

	rigs := reg.List()
	active := make([]NetworkRigRow, 0, len(rigs)+4)
	for _, m := range rigs {
		active = append(active, NetworkRigRow{MetricsRow: m, Simulated: false})
	}
	if len(active) == 0 {
		active = append(active,
			NetworkRigRow{MetricsRow: MetricsRow{WorkerID: "sim-orca", Name: "SIM · Orca rack", HashrateGHS: 142, LastSeenUnix: time.Now().Unix(), IP: "10.0.0.7", Online: true, Source: "remote"}, Simulated: true},
			NetworkRigRow{MetricsRow: MetricsRow{WorkerID: "sim-vega", Name: "SIM · Vega lane", HashrateGHS: 88.5, LastSeenUnix: time.Now().Unix() - 12, IP: "10.0.0.12", Online: true, Source: "remote"}, Simulated: true},
			NetworkRigRow{MetricsRow: MetricsRow{WorkerID: "sim-dust", Name: "SIM · stale worker", HashrateGHS: 22, LastSeenUnix: time.Now().Unix() - 120, IP: "10.0.0.99", Online: false, Source: "remote"}, Simulated: true},
		)
	}

	return NetworkStatsResponse{
		TotalMiners:       baseMiners,
		GlobalHashrateTHS: baseTH,
		PeerConnections:   peers,
		TopMiners:         top,
		ActiveRigs:        active,
		GlobalMock:        true,
		Note:              "Global totals are simulated (HACKME_NETWORK_MOCK). LAN rigs below are real when posted via /api/push_work.",
	}
}

type byGHDesc []MetricsRow

func (s byGHDesc) Len() int           { return len(s) }
func (s byGHDesc) Less(i, j int) bool { return s[i].HashrateGHS > s[j].HashrateGHS }
func (s byGHDesc) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

// RealNetworkStats aggregates LAN workers + local command-node PoH (no random global layer).
func RealNetworkStats(reg *Registry, localNodeID string, local LocalMining) NetworkStatsResponse {
	online := reg.ListOnline()
	var sumRemoteGH float64
	for _, m := range online {
		sumRemoteGH += m.HashrateGHS
	}
	lg := localGH(local)
	totalGH := sumRemoteGH + lg
	globalTH := totalGH / 1000.0
	if globalTH < 0 {
		globalTH = 0
	}

	onlineN := len(online)
	totalMiners := onlineN
	if localNodeID != "" && (local.Running || lg > 0 || onlineN > 0) {
		totalMiners = onlineN + 1
	}
	if totalMiners < 1 && localNodeID != "" {
		totalMiners = 1
	}

	sorted := append([]MetricsRow(nil), online...)
	sort.Sort(byGHDesc(sorted))
	top := make([]string, 0, 5)
	for _, m := range sorted {
		if len(top) >= 5 {
			break
		}
		top = append(top, topMinerAddr(m.WorkerID, localNodeID))
	}
	for i := len(top); i < 5; i++ {
		top = append(top, fmt.Sprintf("HMC-pool-slot-%d", i+1))
	}

	active := make([]NetworkRigRow, 0, len(online))
	for _, m := range online {
		active = append(active, NetworkRigRow{MetricsRow: m, Simulated: false})
	}

	note := "Aggregated: LAN workers (POST /api/push_work) + local command-node PoH throughput. Peer links = online remote workers. Set HACKME_NETWORK_MOCK=1 for legacy simulated globals."
	return NetworkStatsResponse{
		TotalMiners:       totalMiners,
		GlobalHashrateTHS: round4(globalTH),
		PeerConnections:   onlineN,
		TopMiners:         top,
		ActiveRigs:        active,
		GlobalMock:        false,
		Note:              note,
	}
}

func topMinerAddr(workerID, nodeID string) string {
	w := workerID
	if len(w) >= 8 {
		return "HMC-" + w[:min(16, len(w))]
	}
	if nodeID != "" {
		return nodeID
	}
	return "HMC-" + w
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func round4(v float64) float64 {
	return float64(int64(v*10000+0.5)) / 10000
}
