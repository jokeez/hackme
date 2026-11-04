package main

import (
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/poolfuzz"
)

// Fuzz GHS ↔ Dig/Hunt coupling (soft priority + fleet capacity).
// PoH GH/s still pays mining; escrow still pays fuzz. This only steers claim admit
// and publishes honest ETA capacity from live hybrid GHS.

const (
	// Pre-Blake3 hybrid calib was often ≥1 GH/s; Blake3 LIVE wall-rate is ~0.05–0.4 GH/s/GPU.
	// Keep a small floor so noise/canary ≪0.05 does not count as hybrid Dig capacity.
	fuzzGHSHybridMinGHS     = 0.05
	fuzzGHSPriorityStaleSec = 90 // recent PoH / fuzz activity window
	fuzzGHSDigOnlyAdmitPct  = 25 // dig-only admit share when hybrids are online
)

// fuzzGHSHybridMin returns the GH/s floor for hybrid Dig capacity (env override).
func fuzzGHSHybridMin() float64 {
	v := strings.TrimSpace(os.Getenv("HACKME_FUZZ_GHS_HYBRID_MIN"))
	if v == "" {
		return fuzzGHSHybridMinGHS
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil || n < 0 {
		return fuzzGHSHybridMinGHS
	}
	return n
}

func fuzzClaimGHSPriorityEnabled() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_FUZZ_CLAIM_GHS_PRIORITY"))
	if v == "" {
		return true // default ON: real GPU GHS prefers Dig/Hunt leases
	}
	switch strings.ToLower(v) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func fuzzGHSDigOnlyAdmitPercent() int {
	v := strings.TrimSpace(os.Getenv("HACKME_FUZZ_CLAIM_GHS_DIG_ONLY_PCT"))
	if v == "" {
		return fuzzGHSDigOnlyAdmitPct
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fuzzGHSDigOnlyAdmitPct
	}
	if n < 0 {
		return 0
	}
	if n > 100 {
		return 100
	}
	return n
}

func workerPoHFresh(st workerPayoutStat, now int64) bool {
	if now <= 0 {
		now = time.Now().Unix()
	}
	return st.LastPoHSeenUnix > 0 && (now-st.LastPoHSeenUnix) <= fuzzGHSPriorityStaleSec
}

func workerFuzzFresh(st workerPayoutStat, now int64) bool {
	if now <= 0 {
		now = time.Now().Unix()
	}
	return st.LastFuzzSeenUnix > 0 && (now-st.LastFuzzSeenUnix) <= fuzzGHSPriorityStaleSec
}

// workerHybridClaimEligible: recent verified PoH GH/s (not fuzz heartbeat alone).
func workerHybridClaimEligible(st workerPayoutStat, now int64) bool {
	return workerPoHFresh(st, now) && effectiveWorkerHashrateGHS(st) >= fuzzGHSHybridMin()
}

// workerHybridCapacity: digs Dig/Hunt while also mining recently.
func workerHybridCapacity(st workerPayoutStat, now int64) bool {
	return workerHybridClaimEligible(st, now) && workerFuzzFresh(st, now)
}

// fuzzClaimGHSExemptPrefixes returns worker-id prefixes that skip dig-only defer.
// Default: bootstrap dedicated dig fleets (bootstrap-fuzz-*, bootstrap-dig-*).
// Override with HACKME_FUZZ_CLAIM_GHS_EXEMPT_PREFIXES=comma,separated (empty string disables).
// Hunt fleets (worker-hunt-*) are not default-exempt — worker IDs are self-reported;
// operators set HACKME_FUZZ_CLAIM_GHS_EXEMPT_PREFIXES on the coordinator when needed.
func fuzzClaimGHSExemptPrefixes() []string {
	v, set := os.LookupEnv("HACKME_FUZZ_CLAIM_GHS_EXEMPT_PREFIXES")
	if set {
		v = strings.TrimSpace(v)
		if v == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.ToLower(strings.TrimSpace(p))
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return []string{"bootstrap-fuzz-", "bootstrap-dig-"}
}

func fuzzClaimGHSExemptWorker(workerID string) bool {
	id := strings.ToLower(strings.TrimSpace(workerID))
	if id == "" {
		return false
	}
	for _, p := range fuzzClaimGHSExemptPrefixes() {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// allowFuzzClaimByGHS soft-defers dig-only (no recent PoH GH/s) workers when live hybrid
// Dig/Hunt capacity is on the pool. When no hybrid capacity is online, dig-only fleets
// keep full access (bootstrap / dedicated dig). Dedicated bootstrap-fuzz workers are
// always exempt so customer dig fleets are not idle behind the dig-only lottery.
func (m *workManager) allowFuzzClaimByGHS(workerID string, now int64) (bool, string) {
	if m == nil || !fuzzClaimGHSPriorityEnabled() {
		return true, ""
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return true, ""
	}
	if fuzzClaimGHSExemptWorker(workerID) {
		return true, ""
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cap := m.fuzzFleetCapacityUnlocked(now)
	my := workerPayoutStat{}
	if st, ok := m.worker[workerID]; ok {
		my = st
	}
	if cap.HybridWorkersOnline == 0 || cap.FleetHashrateGHS < fuzzGHSHybridMin() {
		return true, ""
	}
	if workerHybridClaimEligible(my, now) {
		return true, ""
	}
	admit := fuzzGHSDigOnlyAdmitPercent()
	if admit >= 100 {
		return true, ""
	}
	if admit <= 0 {
		return false, "ghs_priority_defer"
	}
	// Deterministic slot per worker+10s window so diggers still get a fair minority share.
	slot := int(hashWorkerWindow(workerID, now/10) % 100)
	if slot < admit {
		return true, ""
	}
	return false, "ghs_priority_defer"
}

type fuzzFleetCapacity struct {
	FleetHashrateGHS     float64 `json:"fleet_hashrate_gh_s"`
	HybridWorkersOnline  int     `json:"hybrid_workers_online"`
	DigOnlyWorkersOnline int     `json:"dig_only_workers_online"`
	WorkersOnline        int     `json:"workers_online"`
	EstShardsPerHour     float64 `json:"est_shards_per_hour"`
	EstDigRunsPerHour    float64 `json:"est_dig_runs_per_hour"`
	GHSPriorityEnabled   bool    `json:"ghs_priority_enabled"`
	Note                 string  `json:"note,omitempty"`
}

func (m *workManager) fuzzFleetCapacity(now int64) fuzzFleetCapacity {
	if m == nil {
		return fuzzFleetCapacity{Note: "no work manager"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.fuzzFleetCapacityUnlocked(now)
}

func (m *workManager) fuzzFleetCapacityUnlocked(now int64) fuzzFleetCapacity {
	out := fuzzFleetCapacity{
		GHSPriorityEnabled: fuzzClaimGHSPriorityEnabled(),
		Note:               "hybrid = recent PoH GH/s + recent fuzz; ASAN/WASM still CPU",
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	for _, st := range m.worker {
		// Online for ETA: recent PoH or fuzz activity (not a pure ghost row).
		online := workerPoHFresh(st, now) || workerFuzzFresh(st, now)
		if !online {
			// Fall back to generic last_seen so UI/ETA still sees diggers mid-claim.
			if st.LastSeenUnix <= 0 || (now-st.LastSeenUnix) > fuzzGHSPriorityStaleSec {
				continue
			}
		}
		out.WorkersOnline++
		if workerHybridCapacity(st, now) {
			out.HybridWorkersOnline++
			out.FleetHashrateGHS += effectiveWorkerHashrateGHS(st)
		} else if !workerHybridClaimEligible(st, now) {
			// Dig-only (or PoH-stale): count as dig capacity, not hybrid GHS.
			out.DigOnlyWorkersOnline++
		}
		// PoH-only (fresh mining, no recent fuzz): counted in WorkersOnline only —
		// must not inflate hybrid Dig capacity or throttle dedicated diggers.
	}
	// Heuristic throughput (honest order ETA input, not payout):
	// hybrid digs ~90 shards/h while also mining; dedicated dig ~180 shards/h.
	out.EstShardsPerHour = float64(out.HybridWorkersOnline)*90 + float64(out.DigOnlyWorkersOnline)*180
	out.EstDigRunsPerHour = out.EstShardsPerHour // Dig runs ≈ same claim cadence on pool
	return out
}

// estimateETASeconds maps remaining work units to wall seconds via fleet capacity.
func estimateETASeconds(remaining int, shardsPerHour float64) int64 {
	return poolfuzz.EstimateFleetETASeconds(remaining, shardsPerHour)
}

func cloneCampaignMaps(in []map[string]any) []map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(in))
	for _, src := range in {
		if src == nil {
			continue
		}
		dst := make(map[string]any, len(src)+2)
		for k, v := range src {
			dst[k] = v
		}
		out = append(out, dst)
	}
	return out
}

func intFromProgress(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	default:
		return 0
	}
}

func hashWorkerWindow(workerID string, window int64) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(workerID))
	var b [8]byte
	for i := 0; i < 8; i++ {
		b[i] = byte(window >> (8 * i))
	}
	_, _ = h.Write(b[:])
	return h.Sum32()
}
