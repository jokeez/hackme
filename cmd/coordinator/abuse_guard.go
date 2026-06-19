package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
)

const coordinatorPermabanUntil = int64(math.MaxInt64 >> 1)

type permabanStore struct {
	Workers []string `json:"workers"`
	IPs     []string `json:"ips"`
}

func coordinatorPermabanPath() string {
	if p := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_PERM_BAN_FILE")); p != "" {
		return p
	}
	if dd := strings.TrimSpace(os.Getenv("HACKME_DATA_DIR")); dd != "" {
		return filepath.Join(dd, "coordinator_permaban.json")
	}
	return filepath.Join("data", "coordinator_permaban.json")
}

func loadPermabanStore() permabanStore {
	out := permabanStore{Workers: []string{}, IPs: []string{}}
	if raw, err := os.ReadFile(coordinatorPermabanPath()); err == nil {
		_ = json.Unmarshal(raw, &out)
	}
	for _, part := range strings.Split(os.Getenv("HACKME_COORDINATOR_PERM_BAN_WORKERS"), ",") {
		id := strings.TrimSpace(part)
		if id != "" {
			out.Workers = append(out.Workers, id)
		}
	}
	for _, part := range strings.Split(os.Getenv("HACKME_COORDINATOR_PERM_BAN_IPS"), ",") {
		ip := strings.TrimSpace(part)
		if ip != "" {
			out.IPs = append(out.IPs, ip)
		}
	}
	return out
}

func (m *workManager) initPersistentPermaban() {
	if m == nil {
		return
	}
	store := loadPermabanStore()
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, id := range store.Workers {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		m.permabanWorkerLocked(id, "")
	}
	for _, ip := range store.IPs {
		ip = strings.TrimSpace(ip)
		if ip == "" {
			continue
		}
		m.permabanWorkerLocked("", ip)
	}
}

func persistPermabanEntry(workerID, ipKey string) {
	workerID = strings.TrimSpace(workerID)
	ipKey = strings.TrimSpace(ipKey)
	if workerID == "" && ipKey == "" {
		return
	}
	path := coordinatorPermabanPath()
	store := loadPermabanStore()
	seenW := map[string]struct{}{}
	seenIP := map[string]struct{}{}
	for _, w := range store.Workers {
		seenW[w] = struct{}{}
	}
	for _, ip := range store.IPs {
		seenIP[ip] = struct{}{}
	}
	if workerID != "" {
		if _, ok := seenW[workerID]; !ok {
			store.Workers = append(store.Workers, workerID)
		}
	}
	if ipKey != "" {
		if _, ok := seenIP[ipKey]; !ok {
			store.IPs = append(store.IPs, ipKey)
		}
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	if b, err := json.MarshalIndent(store, "", "  "); err == nil {
		_ = os.WriteFile(path, b, 0o600)
	}
}

func (m *workManager) isHardPermabannedLocked(workerID, ipKey string) bool {
	if s := m.abuse[strings.TrimSpace(workerID)]; s.BannedUntil >= coordinatorPermabanUntil {
		return true
	}
	ipKey = strings.TrimSpace(ipKey)
	if ipKey != "" {
		if s := m.ipAbuse[ipKey]; s.BannedUntil >= coordinatorPermabanUntil {
			return true
		}
	}
	return false
}

func (m *workManager) isHardPermabanned(workerID, ipKey string) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isHardPermabannedLocked(workerID, ipKey)
}

func (m *workManager) clampPaidAttempts(attempts, batch uint64) uint64 {
	if m == nil || attempts == 0 {
		return 0
	}
	cap := m.defaultBatch
	if m.maxClaimBatch > 0 && m.maxClaimBatch < cap {
		cap = m.maxClaimBatch
	}
	if cap > 0 && attempts > cap {
		attempts = cap
	}
	if batch > 0 && attempts > batch {
		attempts = batch
	}
	return attempts
}

func (m *workManager) permabanWorkerLocked(workerID, ipKey string) {
	if m == nil {
		return
	}
	workerID = strings.TrimSpace(workerID)
	if workerID != "" {
		m.abuse[workerID] = workerAbuseState{
			BannedUntil: coordinatorPermabanUntil,
			BadStrikes:  m.badStrikesToBan,
		}
		if st, ok := m.worker[workerID]; ok && ipKey == "" {
			ipKey = st.LastClientIP
		}
	}
	ipKey = strings.TrimSpace(ipKey)
	if ipKey != "" {
		m.ipAbuse[ipKey] = workerAbuseState{
			BannedUntil: coordinatorPermabanUntil,
			BadStrikes:  m.badStrikesToBan,
		}
	}
}

// revokeWorker zeros a worker ledger row, rolls back pool totals, and optionally permabans.
func (m *workManager) revokeWorker(workerID, ipKey string, permaban bool) map[string]any {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return map[string]any{"ok": false, "reason": "worker_id_required"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	st, exists := m.worker[workerID]
	if !exists {
		if !permaban {
			return map[string]any{"ok": false, "reason": "worker_not_found", "worker_id": workerID}
		}
		m.permabanWorkerLocked(workerID, ipKey)
		persistPermabanEntry(workerID, ipKey)
		return map[string]any{
			"ok":        true,
			"worker_id": workerID,
			"permaban":  true,
			"ip_key":    strings.TrimSpace(ipKey),
			"note":      "worker_row_absent_permaban_applied",
		}
	}
	rolledHMC := st.PayoutHMC
	rolledSUP := st.PayoutSUP
	rolledAttempts := st.AcceptedAtt
	if rolledHMC > 0 {
		m.totalPayoutHMC -= rolledHMC
		if m.totalPayoutHMC < 0 {
			m.totalPayoutHMC = 0
		}
	}
	if rolledSUP > 0 {
		m.totalPayoutSUP -= rolledSUP
		if m.totalPayoutSUP < 0 {
			m.totalPayoutSUP = 0
		}
	}
	if rolledAttempts > 0 {
		if m.totalAttempts >= rolledAttempts {
			m.totalAttempts -= rolledAttempts
		} else {
			m.totalAttempts = 0
		}
	}
	for k, rec := range m.active {
		if rec.WorkerID == workerID {
			delete(m.active, k)
		}
	}
	if ipKey == "" {
		ipKey = st.LastClientIP
	}
	delete(m.worker, workerID)
	if permaban {
		m.permabanWorkerLocked(workerID, ipKey)
		persistPermabanEntry(workerID, ipKey)
	}
	return map[string]any{
		"ok":                     true,
		"worker_id":              workerID,
		"rolled_back_payout_hmc": rolledHMC,
		"rolled_back_payout_sup": rolledSUP,
		"rolled_back_attempts":   rolledAttempts,
		"permaban":               permaban,
		"ip_key":                 strings.TrimSpace(ipKey),
	}
}
