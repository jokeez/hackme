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

// foundAuditPersistGapSec coalesces sqlite writes per identity. Writes never run under
// m.mu: busy_timeout(60s) on the hot submit path previously stalled every claim/submit
// and wedged the whole coordinator (tip stall / stats timeout).
const foundAuditPersistGapSec = 30

type foundAuditSnap struct {
	Expected float64
	Actual   float64
	Flagged  bool
}

// markFoundAuditDirtyLocked records a change and schedules an async sqlite write when
// force (flag transition) or the identity's last write is older than the gap.
// Caller holds m.mu. NEVER performs SQLite I/O itself.
func (m *workManager) markFoundAuditDirtyLocked(key string, force bool) {
	st := m.foundAudit[key]
	if st == nil {
		return
	}
	st.dirty = true
	now := time.Now().Unix()
	if !force && now-st.persistedUnix < foundAuditPersistGapSec {
		return
	}
	m.scheduleFoundAuditWriteLocked(key, st, now)
}

func (m *workManager) scheduleFoundAuditWriteLocked(key string, st *foundAuditState, now int64) {
	if st == nil || st.flushing {
		return
	}
	// No sqlite attached: keep dirty for a later attach/flush, but never spawn writers.
	// Spawning finishFoundAuditWrite with dedupDB==nil raced unit tests that call
	// noteFoundAudit*Locked without holding m.mu (and wasted goroutines in prod).
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

func (m *workManager) writeFoundAuditRow(key string, snap foundAuditSnap, now int64) {
	if m == nil || m.dedupDB == nil || key == "" || foundAuditMode() == "off" {
		m.finishFoundAuditWrite(key, snap, now, false)
		return
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
		m.finishFoundAuditWrite(key, snap, now, false)
		return
	}
	m.finishFoundAuditWrite(key, snap, now, true)
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
