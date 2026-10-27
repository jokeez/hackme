package main

import (
	"database/sql"
	"errors"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Issue #8 Phase 3: durable signed-payload / result-hash dedup across coordinator restarts.

func migrateWorkDedupTables(db *sql.DB) error {
	if db == nil {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS work_signed_payload_dedup (
			payload_hash TEXT PRIMARY KEY,
			seen_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS work_result_hash_dedup (
			result_hash TEXT PRIMARY KEY,
			seen_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_work_signed_payload_seen ON work_signed_payload_dedup(seen_at)`,
		`CREATE INDEX IF NOT EXISTS idx_work_result_hash_seen ON work_result_hash_dedup(seen_at)`,
		`CREATE TABLE IF NOT EXISTS worker_payout_lock (
			worker_id TEXT PRIMARY KEY,
			payout_address TEXT NOT NULL,
			seen_at INTEGER NOT NULL DEFAULT 0
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return err
		}
	}
	if err := migrateWorkerAccrualTable(db); err != nil {
		return err
	}
	return ensurePayoutLockSeenAt(db)
}

// ensurePayoutLockSeenAt upgrades installs whose worker_payout_lock predates
// the seen_at column and backfills it, so idle-GC never reaps pre-upgrade rows
// on first boot after this change.
func ensurePayoutLockSeenAt(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(worker_payout_lock)`)
	if err != nil {
		return err
	}
	hasSeenAt := false
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
			rows.Close()
			return err
		}
		if name == "seen_at" {
			hasSeenAt = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !hasSeenAt {
		if _, err := db.Exec(`ALTER TABLE worker_payout_lock ADD COLUMN seen_at INTEGER NOT NULL DEFAULT 0`); err != nil {
			return err
		}
	}
	// Index comes after the upgrade: an old-schema table lacks seen_at while the
	// stmts above run, so creating the index there would fail the migration.
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_worker_payout_lock_seen ON worker_payout_lock(seen_at)`); err != nil {
		return err
	}
	if _, err := db.Exec(`UPDATE worker_payout_lock SET seen_at=? WHERE seen_at=0`, time.Now().Unix()); err != nil {
		return err
	}
	return nil
}

func (m *workManager) attachDedupDB(db *sql.DB) {
	if m == nil || db == nil {
		return
	}
	m.dedupDB = db
	if err := migrateWorkDedupTables(db); err != nil {
		log.Printf("work dedup migrate: %v", err)
		return
	}
	m.gcIdlePayoutLocks()
	m.loadDurableDedup()
	m.loadPayoutLocks()
	m.loadWorkerAccruals()
	m.loadPoolPayDay()
}

func workDedupTTLSec() int64 {
	v := strings.TrimSpace(os.Getenv("HACKME_WORK_DEDUP_TTL_SEC"))
	if v == "" {
		// 24h is enough to block submit replays; 7d maps grew to multi‑million
		// entries and starved the coordinator (GC / nginx upstream timeouts).
		return 24 * 3600
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 3600 {
		return 24 * 3600
	}
	return n
}

func workDedupMaxInMemory() int {
	v := strings.TrimSpace(os.Getenv("HACKME_WORK_DEDUP_MAX_IN_MEMORY"))
	if v == "" {
		return 500_000
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 10_000 {
		return 500_000
	}
	if n > 5_000_000 {
		return 5_000_000
	}
	return n
}

// payoutLockTouchIntervalSec bounds how often an active worker refreshes its
// durable payout-lock seen_at (lock reads happen on every submit path).
const payoutLockTouchIntervalSec = 3600

func workPayoutLockIdleSec() int64 {
	v := strings.TrimSpace(os.Getenv("HACKME_PAYOUT_LOCK_IDLE_SEC"))
	if v == "" {
		// Locks bind a worker_id to its first payout address (report #27);
		// reap only rows no live worker has refreshed for a month.
		return 30 * 24 * 3600
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 30 * 24 * 3600
	}
	if n == 0 {
		return 0 // disabled
	}
	if n < 3600 {
		return 3600
	}
	return n
}

// gcIdlePayoutLocks reaps payout locks whose worker has not been seen for the
// idle horizon. Without it the table grows one row per worker_id ever seen.
func (m *workManager) gcIdlePayoutLocks() {
	if m == nil || m.dedupDB == nil {
		return
	}
	idle := workPayoutLockIdleSec()
	if idle <= 0 {
		return
	}
	cutoff := time.Now().Unix() - idle
	res, err := m.dedupDB.Exec(`DELETE FROM worker_payout_lock WHERE seen_at>0 AND seen_at < ?`, cutoff)
	if err != nil {
		log.Printf("work payout lock gc: %v", err)
		return
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("work payout lock gc: removed %d idle rows (idle_sec=%d)", n, idle)
	}
}

func (m *workManager) loadDurableDedup() {
	if m == nil || m.dedupDB == nil {
		return
	}
	cutoff := time.Now().Unix() - workDedupTTLSec()
	_, _ = m.dedupDB.Exec(`DELETE FROM work_signed_payload_dedup WHERE seen_at < ?`, cutoff)
	_, _ = m.dedupDB.Exec(`DELETE FROM work_result_hash_dedup WHERE seen_at < ?`, cutoff)
	maxN := workDedupMaxInMemory()
	// Newest first so the in-memory cap keeps the hottest anti-replay window.
	rows, err := m.dedupDB.Query(
		`SELECT payload_hash FROM work_signed_payload_dedup WHERE seen_at >= ? ORDER BY seen_at DESC LIMIT ?`,
		cutoff, maxN)
	if err == nil {
		for rows.Next() {
			var h string
			if rows.Scan(&h) == nil && h != "" {
				m.acceptedSignedPayloads[h] = struct{}{}
			}
		}
		rows.Close()
	}
	rows, err = m.dedupDB.Query(
		`SELECT result_hash FROM work_result_hash_dedup WHERE seen_at >= ? ORDER BY seen_at DESC LIMIT ?`,
		cutoff, maxN)
	if err == nil {
		for rows.Next() {
			var h string
			if rows.Scan(&h) == nil && h != "" {
				m.acceptedResultHashes[h] = struct{}{}
			}
		}
		rows.Close()
	}
	log.Printf("work dedup loaded: signed=%d results=%d ttl_sec=%d max_in_memory=%d",
		len(m.acceptedSignedPayloads), len(m.acceptedResultHashes), workDedupTTLSec(), maxN)
}

func (m *workManager) persistSignedPayload(key string) {
	if m == nil || m.dedupDB == nil || key == "" {
		return
	}
	_, _ = m.dedupDB.Exec(
		`INSERT INTO work_signed_payload_dedup(payload_hash, seen_at) VALUES(?,?)
		 ON CONFLICT(payload_hash) DO UPDATE SET seen_at=excluded.seen_at`,
		key, time.Now().Unix())
}

func (m *workManager) loadPayoutLocks() {
	if m == nil || m.dedupDB == nil {
		return
	}
	// Newest-first bounded warm-up; lockedPayoutAddress falls back to the
	// durable row per worker, so bindings evicted from memory stay enforced.
	rows, err := m.dedupDB.Query(
		`SELECT worker_id, payout_address, seen_at FROM worker_payout_lock ORDER BY seen_at DESC LIMIT ?`,
		workDedupMaxInMemory())
	if err != nil {
		log.Printf("work payout lock load: %v", err)
		return
	}
	defer rows.Close()
	type row struct {
		id, addr string
		seen     int64
	}
	var loaded []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.id, &r.addr, &r.seen) != nil {
			continue
		}
		r.id = strings.TrimSpace(r.id)
		r.addr = strings.TrimSpace(r.addr)
		if r.id == "" || r.addr == "" {
			continue
		}
		loaded = append(loaded, r)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.worker == nil {
		m.worker = map[string]workerPayoutStat{}
	}
	for _, r := range loaded {
		if m.maxWorkers > 0 && len(m.worker) >= m.maxWorkers {
			// Newest-first: keep the freshest bindings within the worker cap;
			// the rest fall back to the durable row per read.
			break
		}
		st := m.worker[r.id]
		if strings.TrimSpace(st.PayoutAddress) == "" {
			st.PayoutAddress = r.addr
			m.worker[r.id] = st
		}
		// Seed the touch throttle from durable liveness so a restart does not
		// trigger one UPDATE per worker on the first lock reads.
		if st := m.worker[r.id]; r.seen > 0 && st.LockSeenUnix < r.seen {
			st.LockSeenUnix = r.seen
			m.worker[r.id] = st
		}
	}
	if len(loaded) > 0 {
		log.Printf("work payout locks loaded: %d", len(loaded))
	}
}

// lockedPayoutAddress returns the worker_id binding from memory, falling back
// to the durable lock so a coordinator restart or idle prune cannot free the name.
func (m *workManager) lockedPayoutAddress(workerID string) string {
	if m == nil {
		return ""
	}
	workerID = strings.TrimSpace(workerID)
	m.mu.Lock()
	cur := m.worker[workerID]
	locked := strings.TrimSpace(cur.PayoutAddress)
	db := m.dedupDB
	// Throttled liveness touch: keep the durable seen_at fresh for workers whose
	// lock is being enforced right now, so idle-GC can never reap an active
	// binding (the durable row would otherwise age from first-bind only).
	now := time.Now().Unix()
	touch := locked != "" && now-cur.LockSeenUnix >= payoutLockTouchIntervalSec
	m.mu.Unlock()
	if touch {
		if terr := m.touchPayoutLockSeenAt(workerID); terr == nil {
			// Advance the throttle only after the write succeeded, so a transient
			// DB failure retries on the next lock read instead of after an hour.
			m.mu.Lock()
			st := m.worker[workerID]
			if now > st.LockSeenUnix {
				st.LockSeenUnix = now
				m.worker[workerID] = st
			}
			m.mu.Unlock()
		} else {
			log.Printf("payout lock touch failed: worker_id=%q err=%v", workerID, terr)
		}
	}
	if locked != "" || db == nil || workerID == "" {
		return locked
	}
	var addr string
	var seen int64
	if err := db.QueryRow(`SELECT payout_address, seen_at FROM worker_payout_lock WHERE worker_id=?`, workerID).Scan(&addr, &seen); err != nil {
		if err != sql.ErrNoRows {
			// Fail closed: a broken durable store must not read as "no binding",
			// or identity checks would accept a different payout address.
			log.Printf("payout lock lookup failed: worker_id=%q err=%v", workerID, err)
			return payoutLockLookupFailed
		}
		return ""
	}
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	m.mu.Lock()
	st := m.worker[workerID]
	if strings.TrimSpace(st.PayoutAddress) == "" {
		st.PayoutAddress = addr
		if m.worker == nil {
			m.worker = map[string]workerPayoutStat{}
		}
		m.worker[workerID] = st
	}
	if st := m.worker[workerID]; seen > 0 && st.LockSeenUnix < seen {
		st.LockSeenUnix = seen
		m.worker[workerID] = st
	}
	m.mu.Unlock()
	// First read of a binding that fell out of the warm cache refreshes its
	// liveness right away (UPDATE-only; cannot resurrect anything).
	if terr := m.touchPayoutLockSeenAt(workerID); terr != nil {
		log.Printf("payout lock touch failed: worker_id=%q err=%v", workerID, terr)
	}
	return addr
}

// payoutLockLookupFailed is returned by lockedPayoutAddress when the durable
// store errors: callers treat any non-empty value as "bound to this address",
// so this sentinel fails identity checks closed during a durable-store outage.
const payoutLockLookupFailed = "payout-lock-lookup-failed" // real addresses start with HMC- and hex, so this can never collide

// persistPayoutLock records the first payout address for a worker_id. A later
// different address does not replace it.
// touchPayoutLockSeenAt refreshes liveness for an existing binding only: an
// UPDATE can never resurrect a row an in-flight admin unbind just deleted
// (an INSERT-upsert here would race the DELETE and silently undo the unbind).
func (m *workManager) touchPayoutLockSeenAt(workerID string) error {
	if m == nil || m.dedupDB == nil || workerID == "" {
		return errors.New("payout lock touch: no durable store")
	}
	_, err := m.dedupDB.Exec(`UPDATE worker_payout_lock SET seen_at=? WHERE worker_id=?`,
		time.Now().Unix(), workerID)
	return err
}

func (m *workManager) persistPayoutLock(workerID, addr string) {
	if m == nil || m.dedupDB == nil {
		return
	}
	workerID = strings.TrimSpace(workerID)
	addr = strings.TrimSpace(addr)
	if workerID == "" || addr == "" {
		return
	}
	// First payout address stays bound (report #27); only liveness refreshes.
	// payoutLockMu serializes this write against clearPayoutLock so a claim
	// cannot re-insert a binding an admin unbind just deleted.
	m.payoutLockMu.Lock()
	_, _ = m.dedupDB.Exec(
		`INSERT INTO worker_payout_lock(worker_id, payout_address, seen_at) VALUES(?,?,?)
		 ON CONFLICT(worker_id) DO UPDATE SET seen_at=excluded.seen_at`,
		workerID, addr, time.Now().Unix())
	m.payoutLockMu.Unlock()
}

// clearPayoutLock removes the durable + in-memory payout binding for worker_id
// (admin unbind / report #27 recovery). The durable DELETE runs first and its
// failure is surfaced: answering success while the row survives would silently
// re-bind the worker after the next coordinator restart. Returns the address
// that was cleared.
func (m *workManager) clearPayoutLock(workerID string) (cleared bool, prevAddr string, err error) {
	if m == nil {
		return false, "", nil
	}
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return false, "", nil
	}
	prevAddr = m.lockedPayoutAddress(workerID)
	if prevAddr == payoutLockLookupFailed {
		prevAddr = ""
	}
	m.mu.Lock()
	db := m.dedupDB
	m.mu.Unlock()
	if db != nil {
		// payoutLockMu covers DELETE + memory clear together so an in-flight
		// claim persist cannot land between them and re-insert the binding.
		m.payoutLockMu.Lock()
		res, derr := db.Exec(`DELETE FROM worker_payout_lock WHERE worker_id=?`, workerID)
		if derr == nil {
			m.mu.Lock()
			if m.worker != nil {
				st := m.worker[workerID]
				if strings.TrimSpace(st.PayoutAddress) != "" {
					st.PayoutAddress = ""
					m.worker[workerID] = st
					cleared = true
				}
			}
			m.mu.Unlock()
			if n, _ := res.RowsAffected(); n > 0 {
				cleared = true
			}
		}
		m.payoutLockMu.Unlock()
		if derr != nil {
			log.Printf("payout lock unbind failed: worker_id=%q err=%v", workerID, derr)
			return false, prevAddr, errors.New("durable payout-lock delete failed")
		}
	} else {
		m.mu.Lock()
		if m.worker != nil {
			st := m.worker[workerID]
			if strings.TrimSpace(st.PayoutAddress) != "" {
				st.PayoutAddress = ""
				m.worker[workerID] = st
				cleared = true
			}
		}
		m.mu.Unlock()
	}
	if prevAddr == "" && !cleared {
		return false, "", nil
	}
	return cleared || prevAddr != "", prevAddr, nil
}

func (m *workManager) persistResultHash(key string) {
	if m == nil || m.dedupDB == nil || key == "" {
		return
	}
	_, _ = m.dedupDB.Exec(
		`INSERT INTO work_result_hash_dedup(result_hash, seen_at) VALUES(?,?)
		 ON CONFLICT(result_hash) DO UPDATE SET seen_at=excluded.seen_at`,
		key, time.Now().Unix())
}
