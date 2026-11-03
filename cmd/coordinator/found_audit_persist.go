package main

import (
	"database/sql"
	"log"
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

// foundAuditPersistGapSec bounds sqlite writes per identity: the hot submit path runs under
// m.mu, and a synchronous write per submit (busy_timeout 60s) could stall every submit.
const foundAuditPersistGapSec = 30

// markFoundAuditDirtyLocked records a change; it reaches sqlite at once when force (flag
// transition) or when the identity's last write is older than foundAuditPersistGapSec.
// The rest is flushed on shutdown (flushFoundAuditLocked). Caller holds m.mu.
func (m *workManager) markFoundAuditDirtyLocked(key string, force bool) {
	st := m.foundAudit[key]
	if st == nil {
		return
	}
	st.dirty = true
	if now := time.Now().Unix(); force || now-st.persistedUnix >= foundAuditPersistGapSec {
		m.persistFoundAuditLocked(key, now)
	}
}

// flushFoundAuditLocked writes every unsaved identity (graceful stop). Caller holds m.mu.
func (m *workManager) flushFoundAuditLocked() int {
	n := 0
	now := time.Now().Unix()
	for key, st := range m.foundAudit {
		if st != nil && st.dirty {
			m.persistFoundAuditLocked(key, now)
			n++
		}
	}
	return n
}

func (m *workManager) persistFoundAuditLocked(key string, now int64) {
	if m == nil || m.dedupDB == nil || key == "" || foundAuditMode() == "off" {
		return
	}
	st := m.foundAudit[key]
	if st == nil {
		return
	}
	flagged := 0
	if st.Flagged {
		flagged = 1
	}
	_, err := m.dedupDB.Exec(`
INSERT INTO found_audit (audit_key, expected, actual, flagged, updated_at) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(audit_key) DO UPDATE SET
  expected=excluded.expected,
  actual=excluded.actual,
  flagged=excluded.flagged,
  updated_at=excluded.updated_at`,
		key, st.Expected, st.Actual, flagged, now)
	if err != nil {
		log.Printf("found_audit persist: %v", err)
		return
	}
	st.dirty = false
	st.persistedUnix = now
}

func (m *workManager) deleteFoundAuditLocked(key string) {
	if m == nil || m.dedupDB == nil || key == "" {
		return
	}
	_, _ = m.dedupDB.Exec(`DELETE FROM found_audit WHERE audit_key=?`, key)
}
