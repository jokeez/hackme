package main

import (
	"database/sql"
	"log"
	"strings"
	"sync"
	"time"

	"hackme/internal/chain"
)

// Durable worker accrual (payout_hmc / attempts) across coordinator restarts.
// Settlement already uses session_anchor to avoid double-pay when counters reset;
// without persistence, unsettled session growth is silently abandoned on restart.

const workerAccrualTable = `
CREATE TABLE IF NOT EXISTS worker_accrual (
  worker_id TEXT PRIMARY KEY,
  accepted_ranges INTEGER NOT NULL DEFAULT 0,
  accepted_hits INTEGER NOT NULL DEFAULT 0,
  accepted_attempts INTEGER NOT NULL DEFAULT 0,
  payout_hmc REAL NOT NULL DEFAULT 0,
  payout_units INTEGER NOT NULL DEFAULT 0,
  payout_sup REAL NOT NULL DEFAULT 0,
  payout_address TEXT NOT NULL DEFAULT '',
  signed_submits INTEGER NOT NULL DEFAULT 0,
  last_ghs REAL NOT NULL DEFAULT 0,
  peak_ghs REAL NOT NULL DEFAULT 0,
  last_seen_unix INTEGER NOT NULL DEFAULT 0,
  last_poh_seen_unix INTEGER NOT NULL DEFAULT 0,
  last_fuzz_seen_unix INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0
)`

// Append-only accrual journal (units). Watermark stays in worker_accrual; deltas land here.
const workerAccrualJournalTable = `
CREATE TABLE IF NOT EXISTS accrual_journal (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  worker_id TEXT NOT NULL,
  delta_units INTEGER NOT NULL DEFAULT 0,
  payout_units INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_accrual_journal_worker_ts ON accrual_journal(worker_id, created_at);
`

func migrateWorkerAccrualTable(db *sql.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.Exec(workerAccrualTable); err != nil {
		return err
	}
	_, _ = db.Exec(`ALTER TABLE worker_accrual ADD COLUMN payout_units INTEGER NOT NULL DEFAULT 0`)
	_, _ = db.Exec(`UPDATE worker_accrual SET payout_units = CAST(ROUND(payout_hmc * 100000000) AS INTEGER) WHERE payout_units = 0 AND payout_hmc > 0`)
	_, _ = db.Exec(workerAccrualJournalTable)
	ensureSharePriceJournal(db)
	ensurePoolPayDayTable(db)
	return nil
}

func (m *workManager) loadWorkerAccruals() {
	if m == nil || m.dedupDB == nil {
		return
	}
	rows, err := m.dedupDB.Query(`
SELECT worker_id, accepted_ranges, accepted_hits, accepted_attempts,
       payout_hmc, COALESCE(payout_units,0), payout_sup, payout_address, signed_submits,
       last_ghs, peak_ghs, last_seen_unix, last_poh_seen_unix, last_fuzz_seen_unix
FROM worker_accrual`)
	if err != nil {
		log.Printf("worker accrual load: %v", err)
		return
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.worker == nil {
		m.worker = map[string]workerPayoutStat{}
	}
	var n int
	var sumHMC, sumSUP float64
	for rows.Next() {
		var id, addr string
		var st workerPayoutStat
		if err := rows.Scan(&id, &st.AcceptedRanges, &st.AcceptedHits, &st.AcceptedAtt,
			&st.PayoutHMC, &st.PayoutUnits, &st.PayoutSUP, &addr, &st.SignedSubmits,
			&st.LastHashrateGHS, &st.PeakHashrateGHS, &st.LastSeenUnix, &st.LastPoHSeenUnix, &st.LastFuzzSeenUnix); err != nil {
			log.Printf("worker accrual scan: %v", err)
			continue
		}
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		st.PayoutAddress = strings.TrimSpace(addr)
		// Units authoritative (float is UI mirror). Legacy float→units only when units unset.
		if st.PayoutUnits == 0 && st.PayoutHMC > 0 {
			st.PayoutUnits = chain.HMCToUnits(st.PayoutHMC)
		}
		st.PayoutHMC = chain.UnitsToHMC(st.PayoutUnits)
		// Prefer any already-warm in-memory row (tests); else restore durable.
		if cur, ok := m.worker[id]; ok && (cur.PayoutHMC > 0 || cur.AcceptedAtt > 0) {
			continue
		}
		if m.shareStrict {
			// PPS era: do not rehydrate FORCE/calib GH into pool M; re-earn via shares/wall.
			st.LastHashrateGHS = 0
			st.PeakHashrateGHS = 0
		}
		m.worker[id] = st
		sumHMC += st.PayoutHMC
		sumSUP += st.PayoutSUP
		n++
	}
	if err := rows.Err(); err != nil {
		log.Printf("worker accrual rows: %v", err)
	}
	// Rebuild fleet totals from durable rows when memory was empty/ephemeral.
	if n > 0 && m.totalPayoutHMC == 0 && m.totalPayoutSUP == 0 {
		m.totalPayoutHMC = sumHMC
		m.totalPayoutSUP = sumSUP
	}
	if n > 0 {
		log.Printf("worker accrual: restored %d worker(s) payout_hmc=%.6f payout_sup=%.6f", n, m.totalPayoutHMC, m.totalPayoutSUP)
	}
}

func (m *workManager) fetchDurableAccrual(workerID string) (workerPayoutStat, bool) {
	var st workerPayoutStat
	if m == nil || m.dedupDB == nil {
		return st, false
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return st, false
	}
	var addr string
	err := m.dedupDB.QueryRow(`
SELECT accepted_ranges, accepted_hits, accepted_attempts,
       payout_hmc, COALESCE(payout_units,0), payout_sup, payout_address, signed_submits,
       last_ghs, peak_ghs, last_seen_unix, last_poh_seen_unix, last_fuzz_seen_unix
FROM worker_accrual WHERE worker_id=?`, workerID).Scan(
		&st.AcceptedRanges, &st.AcceptedHits, &st.AcceptedAtt,
		&st.PayoutHMC, &st.PayoutUnits, &st.PayoutSUP, &addr, &st.SignedSubmits,
		&st.LastHashrateGHS, &st.PeakHashrateGHS, &st.LastSeenUnix, &st.LastPoHSeenUnix, &st.LastFuzzSeenUnix)
	if err != nil {
		return st, false
	}
	st.PayoutAddress = strings.TrimSpace(addr)
	if st.PayoutUnits == 0 && st.PayoutHMC > 0 {
		st.PayoutUnits = chain.HMCToUnits(st.PayoutHMC)
	}
	st.PayoutHMC = chain.UnitsToHMC(st.PayoutUnits)
	return st, true
}

// prefetchWorkerAccrual hydrates a missing in-memory row from durable store.
// Must NOT run while holding m.mu (SQLite under the submit lock stalls the pool).
func (m *workManager) prefetchWorkerAccrual(workerID string) {
	if m == nil || m.dedupDB == nil {
		return
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return
	}
	m.mu.Lock()
	_, exists := m.worker[workerID]
	m.mu.Unlock()
	if exists {
		return
	}
	st, ok := m.fetchDurableAccrual(workerID)
	if !ok {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.worker == nil {
		m.worker = map[string]workerPayoutStat{}
	}
	if _, exists := m.worker[workerID]; exists {
		return
	}
	if m.shareStrict {
		st.LastHashrateGHS = 0
		st.PeakHashrateGHS = 0
	}
	m.worker[workerID] = st
	m.totalPayoutHMC += st.PayoutHMC
	m.totalPayoutSUP += st.PayoutSUP
}

// ensureWorkerAccrualLocked returns the in-memory accrual row (possibly empty).
// Caller must hold m.mu. Prefer prefetchWorkerAccrual before taking the lock.
func (m *workManager) ensureWorkerAccrualLocked(workerID string) workerPayoutStat {
	if m == nil {
		return workerPayoutStat{}
	}
	if m.worker == nil {
		m.worker = map[string]workerPayoutStat{}
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return workerPayoutStat{}
	}
	return m.worker[workerID]
}

var (
	accrualPersistMu   sync.Mutex
	accrualPersistLast = map[string]int64{}
)

func accrualPersistMinGapSec() int64 {
	return 2 // throttle DB writes on the hot submit path
}

// deleteWorkerAccrualLocked removes durable unpaid for workerID.
// Caller must hold m.mu. Safe no-op when DB is nil.
func (m *workManager) deleteWorkerAccrualLocked(workerID string) {
	if m == nil || m.dedupDB == nil {
		return
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return
	}
	if _, err := m.dedupDB.Exec(`DELETE FROM worker_accrual WHERE worker_id=?`, workerID); err != nil {
		// Avoid logging raw worker_id (code-scanning go/log-injection); token is sanitized.
		log.Printf("worker accrual revoke id=%s err=%v", sanitizeLogToken(workerID), err)
	}
	accrualPersistMu.Lock()
	delete(accrualPersistLast, workerID)
	accrualPersistMu.Unlock()
}

// bumpAccrualEpochLocked invalidates in-flight persist snapshots for workerID.
// Caller must hold m.mu. Prune must NOT call this — durable unpaid must survive UI prune.
func (m *workManager) bumpAccrualEpochLocked(workerID string) {
	if m == nil {
		return
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return
	}
	if m.accrualEpoch == nil {
		m.accrualEpoch = map[string]uint64{}
	}
	m.accrualEpoch[workerID]++
	if m.accrualEpoch[workerID] == 0 {
		m.accrualEpoch[workerID] = 1
	}
}

func (m *workManager) accrualEpochLocked(workerID string) uint64 {
	if m == nil || m.accrualEpoch == nil {
		return 0
	}
	return m.accrualEpoch[workerID]
}

func (m *workManager) persistWorkerAccrual(workerID string, force bool) {
	if m == nil || m.dedupDB == nil {
		return
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return
	}
	now := time.Now().Unix()
	if !force {
		accrualPersistMu.Lock()
		last := accrualPersistLast[workerID]
		if now-last < accrualPersistMinGapSec() {
			accrualPersistMu.Unlock()
			return
		}
		accrualPersistLast[workerID] = now
		accrualPersistMu.Unlock()
	}

	m.mu.Lock()
	st, ok := m.worker[workerID]
	if !ok {
		m.mu.Unlock()
		// Prune removed memory on purpose — leave durable unpaid alone.
		return
	}
	epoch := m.accrualEpochLocked(workerID)
	snap := st
	m.mu.Unlock()

	// Monotonic upsert: a delayed goroutine must not overwrite a newer accepted_attempts row.
	pu := snap.PayoutUnits
	if pu == 0 && snap.PayoutHMC > 0 {
		pu = chain.HMCToUnits(snap.PayoutHMC)
	}
	hmcMirror := chain.UnitsToHMC(pu)

	tx, err := m.dedupDB.Begin()
	if err != nil {
		log.Printf("worker accrual begin id=%s err=%v", sanitizeLogToken(workerID), err)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	var prevUnits uint64
	_ = tx.QueryRow(`SELECT COALESCE(payout_units,0) FROM worker_accrual WHERE worker_id=?`, workerID).Scan(&prevUnits)
	res, err := tx.Exec(`
INSERT INTO worker_accrual (
  worker_id, accepted_ranges, accepted_hits, accepted_attempts,
  payout_hmc, payout_units, payout_sup, payout_address, signed_submits,
  last_ghs, peak_ghs, last_seen_unix, last_poh_seen_unix, last_fuzz_seen_unix, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(worker_id) DO UPDATE SET
  accepted_ranges=excluded.accepted_ranges,
  accepted_hits=excluded.accepted_hits,
  accepted_attempts=excluded.accepted_attempts,
  payout_hmc=excluded.payout_hmc,
  payout_units=excluded.payout_units,
  payout_sup=excluded.payout_sup,
  payout_address=excluded.payout_address,
  signed_submits=excluded.signed_submits,
  last_ghs=excluded.last_ghs,
  peak_ghs=excluded.peak_ghs,
  last_seen_unix=excluded.last_seen_unix,
  last_poh_seen_unix=excluded.last_poh_seen_unix,
  last_fuzz_seen_unix=excluded.last_fuzz_seen_unix,
  updated_at=excluded.updated_at
WHERE excluded.accepted_attempts >= worker_accrual.accepted_attempts
`, workerID, snap.AcceptedRanges, snap.AcceptedHits, snap.AcceptedAtt,
		hmcMirror, pu, snap.PayoutSUP, strings.TrimSpace(snap.PayoutAddress), snap.SignedSubmits,
		snap.LastHashrateGHS, snap.PeakHashrateGHS, snap.LastSeenUnix, snap.LastPoHSeenUnix, snap.LastFuzzSeenUnix, now)
	if err != nil {
		log.Printf("worker accrual persist id=%s err=%v", sanitizeLogToken(workerID), err)
		return
	}
	n, _ := res.RowsAffected()
	if n > 0 && pu > prevUnits {
		delta := pu - prevUnits
		if _, jerr := tx.Exec(`
INSERT INTO accrual_journal (worker_id, delta_units, payout_units, created_at)
VALUES (?, ?, ?, ?)`, workerID, delta, pu, now); jerr != nil {
			log.Printf("accrual journal id=%s err=%v", sanitizeLogToken(workerID), jerr)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.Printf("worker accrual commit id=%s err=%v", sanitizeLogToken(workerID), err)
		return
	}
	committed = true

	// Revoke may have DELETE'd while we held the snapshot; purge any resurrection.
	m.mu.Lock()
	cur := m.accrualEpochLocked(workerID)
	if cur != epoch {
		m.deleteWorkerAccrualLocked(workerID)
	}
	m.mu.Unlock()
}

// flushAllWorkerAccruals force-persists every in-memory row (SIGTERM / graceful stop).
func (m *workManager) flushAllWorkerAccruals() {
	if m == nil || m.dedupDB == nil {
		return
	}
	m.mu.Lock()
	ids := make([]string, 0, len(m.worker))
	for id := range m.worker {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.persistWorkerAccrual(id, true)
	}
	if len(ids) > 0 {
		log.Printf("worker accrual: flushed %d worker(s) on shutdown", len(ids))
	}
}
