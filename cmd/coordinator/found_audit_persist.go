package main

import (
	"database/sql"
	"log"
	"sync"
	"time"
)

// Durable found-audit counters so a coordinator restart cannot wipe withhold evidence
// (audit H2). Same dedupDB as pool_pay_day / signed-payload dedup.

const foundAuditTable = `
CREATE TABLE IF NOT EXISTS found_audit (
  audit_key TEXT PRIMARY KEY,
  expected REAL NOT NULL DEFAULT 0,
  actual REAL NOT NULL DEFAULT 0,
  flagged INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0
)`

func ensureFoundAuditTable(db *sql.DB) {
	if db == nil {
		return
	}
	if _, err := db.Exec(foundAuditTable); err != nil {
		log.Printf("found_audit schema: %v", err)
	}
}

func (m *workManager) loadFoundAudit() {
	if m == nil || m.dedupDB == nil {
		return
	}
	ensureFoundAuditTable(m.dedupDB)
	rows, err := m.dedupDB.Query(`SELECT audit_key, expected, actual, flagged FROM found_audit`)
	if err != nil {
		log.Printf("found_audit load: %v", err)
		return
	}
	defer rows.Close()
	if m.foundAudit == nil {
		m.foundAudit = make(map[string]*foundAuditState)
	}
	n := 0
	for rows.Next() {
		var key string
		var expected, actual float64
		var flaggedInt int
		if err := rows.Scan(&key, &expected, &actual, &flaggedInt); err != nil {
			log.Printf("found_audit load row: %v", err)
			continue
		}
		if key == "" {
			continue
		}
		if len(m.foundAudit) >= foundAuditMaxEntries {
			break
		}
		m.foundAudit[key] = &foundAuditState{
			Expected: expected,
			Actual:   actual,
			Flagged:  flaggedInt != 0,
		}
		n++
	}
	if err := rows.Err(); err != nil {
		log.Printf("found_audit load rows: %v", err)
	}
	if n > 0 {
		log.Printf("found_audit: restored %d identities from sqlite", n)
	}
}

// foundAuditPersistGapSec coalesces sqlite writes per identity. Routine counter bumps
// stay async (never under m.mu: busy_timeout wedged claim/submit). Flag transitions
// (force) write synchronously after a brief m.mu release so SIGKILL cannot wipe H2 evidence.
const foundAuditPersistGapSec = 30

type foundAuditSnap struct {
	Expected float64
	Actual   float64
	Flagged  bool
}

// markFoundAuditDirtyLocked records a change and persists it.
// force (flag transition): durable sync write — briefly releases m.mu for SQLite only.
// otherwise: async coalesced write. Caller holds m.mu.
func (m *workManager) markFoundAuditDirtyLocked(key string, force bool) {
	st := m.foundAudit[key]
	if st == nil {
		return
	}
	st.dirty = true
	now := time.Now().Unix()
	if force {
		m.persistFoundAuditForceLocked(key, st, now)
		return
	}
	if now-st.persistedUnix < foundAuditPersistGapSec {
		return
	}
	m.scheduleFoundAuditWriteLocked(key, st, now)
}

// persistFoundAuditForceLocked makes a flag transition survive crash/SIGKILL.
// Caller holds m.mu; this unlocks for the SQLite round-trip then re-locks.
func (m *workManager) persistFoundAuditForceLocked(key string, st *foundAuditState, now int64) {
	if st == nil {
		return
	}
	if m.dedupDB == nil || foundAuditMode() == "off" {
		return
	}
	// Drain any in-flight async writer so it cannot clobber this snap with a stale row.
	for st.flushing {
		m.mu.Unlock()
		m.waitFoundAuditPersisted()
		m.mu.Lock()
		st = m.foundAudit[key]
		if st == nil {
			return
		}
	}
	snap := foundAuditSnap{Expected: st.Expected, Actual: st.Actual, Flagged: st.Flagged}
	st.flushing = true
	m.mu.Unlock()
	ok := m.execFoundAuditRow(key, snap, now)
	m.mu.Lock()
	st = m.foundAudit[key]
	if st == nil {
		return
	}
	st.flushing = false
	if ok {
		st.persistedUnix = now
		if st.Expected == snap.Expected && st.Actual == snap.Actual && st.Flagged == snap.Flagged {
			st.dirty = false
			return
		}
	}
	if st.dirty {
		m.scheduleFoundAuditWriteLocked(key, st, time.Now().Unix())
	}
}

func (m *workManager) scheduleFoundAuditWriteLocked(key string, st *foundAuditState, now int64) {
	if st == nil || st.flushing {
		return
	}
	// No sqlite attached: keep dirty for a later attach/flush, but never spawn writers.
	if m.dedupDB == nil {
		return
	}
	st.flushing = true
	snap := foundAuditSnap{Expected: st.Expected, Actual: st.Actual, Flagged: st.Flagged}
	if m.foundAuditPersistWG == nil {
		m.foundAuditPersistWG = &sync.WaitGroup{}
	}
	m.foundAuditPersistWG.Add(1)
	go func() {
		defer m.foundAuditPersistWG.Done()
		m.writeFoundAuditRow(key, snap, now)
	}()
}

func (m *workManager) execFoundAuditRow(key string, snap foundAuditSnap, now int64) bool {
	if m == nil || m.dedupDB == nil || key == "" || foundAuditMode() == "off" {
		return false
	}
	flagged := 0
	if snap.Flagged {
		flagged = 1
	}
	_, err := m.dedupDB.Exec(`
INSERT INTO found_audit (audit_key, expected, actual, flagged, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(audit_key) DO UPDATE SET
  expected=excluded.expected,
  actual=excluded.actual,
  flagged=excluded.flagged,
  updated_at=excluded.updated_at`,
		key, snap.Expected, snap.Actual, flagged, now)
	if err != nil {
		log.Printf("found_audit persist: %v", err)
		return false
	}
	return true
}

func (m *workManager) writeFoundAuditRow(key string, snap foundAuditSnap, now int64) {
	ok := m.execFoundAuditRow(key, snap, now)
	m.finishFoundAuditWrite(key, snap, now, ok)
}

func (m *workManager) finishFoundAuditWrite(key string, snap foundAuditSnap, now int64, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.foundAudit[key]
	if st == nil {
		return
	}
	st.flushing = false
	if ok {
		st.persistedUnix = now
		// Clear dirty only if memory still matches the snap we wrote; else reschedule.
		if st.Expected == snap.Expected && st.Actual == snap.Actual && st.Flagged == snap.Flagged {
			st.dirty = false
			return
		}
	}
	if st.dirty {
		m.scheduleFoundAuditWriteLocked(key, st, time.Now().Unix())
	}
}

// flushFoundAuditLocked snapshots dirty rows and writes them asynchronously.
// Prefer waitFoundAuditPersisted after unlock on shutdown/tests. Returns dirty count queued.
// Caller holds m.mu.
func (m *workManager) flushFoundAuditLocked() int {
	n := 0
	now := time.Now().Unix()
	for key, st := range m.foundAudit {
		if st == nil || !st.dirty {
			continue
		}
		m.scheduleFoundAuditWriteLocked(key, st, now)
		n++
	}
	return n
}

// waitFoundAuditPersisted blocks until in-flight found_audit writes finish (tests/shutdown).
func (m *workManager) waitFoundAuditPersisted() {
	if m == nil || m.foundAuditPersistWG == nil {
		return
	}
	m.foundAuditPersistWG.Wait()
}

func (m *workManager) deleteFoundAuditLocked(key string) {
	if m == nil || m.dedupDB == nil || key == "" {
		return
	}
	// Async: eviction runs under m.mu on the hot path.
	db := m.dedupDB
	go func() {
		_, _ = db.Exec(`DELETE FROM found_audit WHERE audit_key=?`, key)
	}()
}
