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

func (m *workManager) persistFoundAuditLocked(key string) {
	if m == nil || m.dedupDB == nil || key == "" || foundAuditMode() == "off" {
		return
	}
	st := m.foundAudit[key]
	if st == nil {
		return
	}
	ensureFoundAuditTable(m.dedupDB)
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
		key, st.Expected, st.Actual, flagged, time.Now().Unix())
	if err != nil {
		log.Printf("found_audit persist: %v", err)
	}
}

func (m *workManager) deleteFoundAuditLocked(key string) {
	if m == nil || m.dedupDB == nil || key == "" {
		return
	}
	_, _ = m.dedupDB.Exec(`DELETE FROM found_audit WHERE audit_key=?`, key)
}
