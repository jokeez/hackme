package main

import (
	"math"
	"strings"
)

const coordinatorPermabanUntil = int64(math.MaxInt64 >> 1)

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
		return map[string]any{"ok": false, "reason": "worker_not_found", "worker_id": workerID}
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
