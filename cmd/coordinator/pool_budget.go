package main

import (
	"database/sql"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Pool daily pay budget keeps SHARE_STRICT accrual solvent vs chain emission.
// Default: base_reward × (86400 / block_sec) ≈ 0.01 × 2880 = 28.8 HMC/day.
// Override: HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC (0 disables the cap).
// Accrued-today is persisted in SQLite so coordinator restarts cannot mint a fresh day budget.

const (
	defaultPoolBlockSec = 30.0
)

const poolPayDayTable = `
CREATE TABLE IF NOT EXISTS pool_pay_day (
  day_id INTEGER PRIMARY KEY,
  accrued_hmc REAL NOT NULL DEFAULT 0,
  clip_events INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0
)`

func poolBlockSec() float64 {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_POOL_BLOCK_SEC"))
	if v == "" {
		return defaultPoolBlockSec
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil || x < 1 || x > 600 {
		return defaultPoolBlockSec
	}
	return x
}

func (m *workManager) poolDailyBudgetHMC() float64 {
	if m == nil {
		return 0
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC")); v != "" {
		x, err := strconv.ParseFloat(v, 64)
		if err == nil && x >= 0 {
			return x
		}
	}
	base := m.baseRewardHMC
	if base <= 0 {
		base = 0.01
	}
	sec := poolBlockSec()
	if sec <= 0 {
		sec = defaultPoolBlockSec
	}
	return base * (86400.0 / sec)
}

func ensurePoolPayDayTable(db *sql.DB) {
	if db == nil {
		return
	}
	if _, err := db.Exec(poolPayDayTable); err != nil {
		log.Printf("pool_pay_day schema: %v", err)
	}
}

func (m *workManager) loadPoolPayDay() {
	if m == nil || m.dedupDB == nil {
		return
	}
	ensurePoolPayDayTable(m.dedupDB)
	day := utcDayID(nowUnix())
	var accrued float64
	var clips uint64
	err := m.dedupDB.QueryRow(`SELECT accrued_hmc, clip_events FROM pool_pay_day WHERE day_id=?`, day).
		Scan(&accrued, &clips)
	if err == sql.ErrNoRows {
		m.poolPayDayID = day
		m.poolPayAccruedDay = 0
		return
	}
	if err != nil {
		log.Printf("pool_pay_day load: %v", err)
		return
	}
	m.poolPayDayID = day
	m.poolPayAccruedDay = accrued
	m.poolBudgetClipped = clips
}

func (m *workManager) persistPoolPayDayLocked(now int64) {
	if m == nil || m.dedupDB == nil {
		return
	}
	ensurePoolPayDayTable(m.dedupDB)
	_, err := m.dedupDB.Exec(`
INSERT INTO pool_pay_day (day_id, accrued_hmc, clip_events, updated_at) VALUES (?, ?, ?, ?)
ON CONFLICT(day_id) DO UPDATE SET
  accrued_hmc=excluded.accrued_hmc,
  clip_events=excluded.clip_events,
  updated_at=excluded.updated_at`,
		m.poolPayDayID, m.poolPayAccruedDay, m.poolBudgetClipped, now)
	if err != nil {
		log.Printf("pool_pay_day persist: %v", err)
	}
}

func (m *workManager) resetPoolPayDayIfNeeded(now int64) {
	day := utcDayID(now)
	if m.poolPayDayID != day {
		m.poolPayDayID = day
		m.poolPayAccruedDay = 0
		// Do not zero clip_events lifetime counter across days — only day accrued resets.
	}
}

// applyPoolDailyBudget caps a submit payout so rolling UTC-day accrual stays ≤ budget.
// Caller holds m.mu. Budget 0 disables the cap.
func (m *workManager) applyPoolDailyBudget(payout float64, now int64) float64 {
	if m == nil || payout <= 0 {
		return payout
	}
	budget := m.poolDailyBudgetHMC()
	if budget <= 0 {
		return payout
	}
	m.resetPoolPayDayIfNeeded(now)
	remain := budget - m.poolPayAccruedDay
	if remain <= 0 {
		m.poolBudgetClipped++
		m.persistPoolPayDayLocked(now)
		return 0
	}
	if payout > remain {
		payout = remain
		m.poolBudgetClipped++
	}
	m.poolPayAccruedDay += payout
	m.persistPoolPayDayLocked(now)
	return payout
}

func (m *workManager) poolBudgetStats() map[string]any {
	if m == nil {
		return map[string]any{"enabled": false}
	}
	budget := m.poolDailyBudgetHMC()
	remain := budget - m.poolPayAccruedDay
	if remain < 0 {
		remain = 0
	}
	return map[string]any{
		"enabled":             budget > 0,
		"daily_budget_hmc":    budget,
		"accrued_today_hmc":   m.poolPayAccruedDay,
		"remaining_today_hmc": remain,
		"budget_clip_events":  m.poolBudgetClipped,
		"block_sec":           poolBlockSec(),
		"persisted":           m.dedupDB != nil,
		"note":                "accrual capped to chain emission schedule; settle still requires treasury float",
	}
}

func nowUnix() int64 { return time.Now().Unix() }
