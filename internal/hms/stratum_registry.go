package hms

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	stratumSubmitWindowSec   = int64(60)
	minSubmitsForHashrate    = 3
	stratumHashrateSmoothing = 0.35 // EMA weight for new sample
)

// StratumRegistry tracks live Stratum TCP peers (ASIC gateways / simulators).
type StratumRegistry struct {
	mu    sync.RWMutex
	peers map[string]*stratumPeer
}

type stratumPeer struct {
	WorkerID     string
	RemoteAddr   string
	ConnectedAt  int64
	LastSeen     int64
	MeasuredTH   float64
	SharesOK     uint64
	SharesFail   uint64
	SubmitTimes  []int64
	LastWorkMode string
}

func NewStratumRegistry() *StratumRegistry {
	return &StratumRegistry{peers: make(map[string]*stratumPeer)}
}

func (r *StratumRegistry) Connect(remote, workerID string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := remote + "|" + workerID + "|" + strconv.FormatInt(time.Now().UnixNano(), 36)
	r.peers[id] = &stratumPeer{
		WorkerID:    strings.TrimSpace(workerID),
		RemoteAddr:  remote,
		ConnectedAt: time.Now().Unix(),
		LastSeen:    time.Now().Unix(),
	}
	return id
}

func (r *StratumRegistry) Disconnect(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.peers, id)
}

func (r *StratumRegistry) Touch(id, workerID, password string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.peers[id]
	if !ok {
		return
	}
	p.LastSeen = time.Now().Unix()
	if w := strings.TrimSpace(workerID); w != "" {
		p.WorkerID = w
	}
	_ = password // hashrate is measured from submits only, not pool password
}

func (r *StratumRegistry) SetWorkMode(id, mode string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if p, ok := r.peers[id]; ok {
		p.LastWorkMode = mode
	}
}

// RecordSubmit counts a Stratum mining.submit (used for measured hashrate).
func (r *StratumRegistry) RecordSubmit(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, exists := r.peers[id]
	if !exists {
		return
	}
	now := time.Now().Unix()
	p.LastSeen = now
	p.SubmitTimes = appendRecentSubmit(p.SubmitTimes, now)
	refreshMeasuredTH(p, now)
}

func (r *StratumRegistry) RecordShare(id string, ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, exists := r.peers[id]
	if !exists {
		return
	}
	p.LastSeen = time.Now().Unix()
	if ok {
		p.SharesOK++
	} else {
		p.SharesFail++
	}
}

func appendRecentSubmit(times []int64, now int64) []int64 {
	times = append(times, now)
	cutoff := now - stratumSubmitWindowSec
	i := 0
	for i < len(times) && times[i] < cutoff {
		i++
	}
	if i > 0 {
		times = times[i:]
	}
	if len(times) > 512 {
		times = times[len(times)-512:]
	}
	return times
}

// instantTHFromSubmits estimates TH/s from submit rate (diff-1 share ≈ 2^32 hashes).
func instantTHFromSubmits(times []int64, now int64) float64 {
	if len(times) < minSubmitsForHashrate {
		return 0
	}
	cutoff := now - stratumSubmitWindowSec
	n := 0
	var span int64
	oldest := now
	for _, t := range times {
		if t >= cutoff {
			n++
			if t < oldest {
				oldest = t
			}
		}
	}
	if n < minSubmitsForHashrate {
		return 0
	}
	span = now - oldest
	if span < 1 {
		span = 1
	}
	submitsPerSec := float64(n) / float64(span)
	return submitsPerSec * 4294967296.0 / 1e12
}

func refreshMeasuredTH(p *stratumPeer, now int64) {
	instant := instantTHFromSubmits(p.SubmitTimes, now)
	if instant <= 0 {
		return
	}
	if p.MeasuredTH <= 0 {
		p.MeasuredTH = instant
		return
	}
	p.MeasuredTH = stratumHashrateSmoothing*instant + (1-stratumHashrateSmoothing)*p.MeasuredTH
}

func measuredPeerTH(p *stratumPeer, now int64) float64 {
	if now-p.LastSeen > stratumSubmitWindowSec {
		return 0
	}
	refreshMeasuredTH(p, now)
	return math.Max(0, p.MeasuredTH)
}

func submitsPerMin(p *stratumPeer, now int64) float64 {
	cutoff := now - stratumSubmitWindowSec
	n := 0
	for _, t := range p.SubmitTimes {
		if t >= cutoff {
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(n) / float64(stratumSubmitWindowSec) * 60.0
}

// SharesByWorker aggregates in-memory share counters keyed by worker_id.
func (r *StratumRegistry) SharesByWorker() map[string][2]uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := map[string][2]uint64{}
	for _, p := range r.peers {
		wid := strings.TrimSpace(p.WorkerID)
		if wid == "" {
			continue
		}
		cur := out[wid]
		cur[0] += p.SharesOK
		cur[1] += p.SharesFail
		out[wid] = cur
	}
	return out
}

func (r *StratumRegistry) EffectiveTHByWorker() map[string]float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now().Unix()
	out := map[string]float64{}
	for _, p := range r.peers {
		if now-p.LastSeen > stratumSubmitWindowSec {
			continue
		}
		wid := strings.TrimSpace(p.WorkerID)
		if wid == "" {
			continue
		}
		th := measuredPeerTH(p, now)
		if th > out[wid] {
			out[wid] = th
		}
	}
	return out
}

func (r *StratumRegistry) Snapshot(warmUnits func(string) uint64) (connected int, totalMeasured float64, peers []map[string]any) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	now := time.Now().Unix()
	byWorker := map[string]map[string]any{}

	for _, p := range r.peers {
		if now-p.LastSeen > stratumSubmitWindowSec {
			continue
		}
		wid := strings.TrimSpace(p.WorkerID)
		if wid == "" {
			wid = "unknown"
		}
		th := measuredPeerTH(p, now)
		spm := submitsPerMin(p, now)

		cur, ok := byWorker[wid]
		if !ok {
			cur = map[string]any{
				"worker_id":       wid,
				"remote_addr":     p.RemoteAddr,
				"connected_at":    p.ConnectedAt,
				"last_seen_unix":  p.LastSeen,
				"hashrate_th":     0.0,
				"measured_th":     0.0,
				"submits_per_min": 0.0,
				"shares_ok":       uint64(0),
				"shares_fail":     uint64(0),
				"work_mode":       p.LastWorkMode,
			}
		}
		if p.LastSeen > cur["last_seen_unix"].(int64) {
			cur["last_seen_unix"] = p.LastSeen
			cur["remote_addr"] = p.RemoteAddr
		}
		if p.LastWorkMode != "" {
			cur["work_mode"] = p.LastWorkMode
		}
		cur["hashrate_th"] = maxFloat(cur["hashrate_th"].(float64), th)
		cur["measured_th"] = cur["hashrate_th"]
		cur["submits_per_min"] = maxFloat(cur["submits_per_min"].(float64), spm)
		cur["shares_ok"] = cur["shares_ok"].(uint64) + p.SharesOK
		cur["shares_fail"] = cur["shares_fail"].(uint64) + p.SharesFail
		byWorker[wid] = cur
	}

	keys := make([]string, 0, len(byWorker))
	for k := range byWorker {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		cur := byWorker[k]
		if warmUnits != nil {
			cur["warm_accrual_hms"] = float64(warmUnits(k)) / HMSUnitsPerCoin
			cur["warm_accrual_units"] = warmUnits(k)
		}
		connected++
		peers = append(peers, cur)
	}

	totalMeasured = 0
	for _, cur := range peers {
		totalMeasured += cur["hashrate_th"].(float64)
	}
	return connected, totalMeasured, peers
}

func maxFloat(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

// parseReportedTH kept for legacy tests; production hashrate ignores pool password.
func parseReportedTH(password string) float64 {
	password = strings.TrimSpace(strings.ToLower(password))
	if password == "" || password == "x" {
		return 0
	}
	for _, part := range strings.FieldsFunc(password, func(r rune) bool {
		return r == ';' || r == ',' || r == ' '
	}) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "th=") {
			part = strings.TrimPrefix(part, "th=")
		}
		part = strings.TrimSuffix(part, "th")
		part = strings.TrimSuffix(part, "ths")
		if v, err := strconv.ParseFloat(part, 64); err == nil && v > 0 {
			return v
		}
	}
	return 0
}
