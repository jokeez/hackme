package main

import (
	"database/sql"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Pool daily pay budget keeps SHARE_STRICT accrual solvent vs chain emission.
// Default: base_reward × (86400 / block_sec) ≈ 0.01 × 2880 = 28.8 HMC/day.
// Override: HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC (0 disables the cap).
// Accrued-today is persisted in SQLite so coordinator restarts cannot mint a fresh day budget.
//
// Pacing: the budget unlocks evenly over the UTC day (plus a burst allowance,
// HACKME_COORDINATOR_POOL_BUDGET_BURST_SEC, default 3600; 86400 = old first-come day cap).
// Without it a large fleet at 00:00 UTC drained the whole day by ~07:30 and everyone mining
// later accrued nothing.

const (
	defaultPoolBlockSec       = 30.0
	defaultPoolBudgetBurstSec = 3600
	// poolPayPersistGapSec throttles pool_pay_day sqlite writes on the hot submit path.
	// Writes run async — never block m.mu on busy_timeout (was wedging the coordinator).
	poolPayPersistGapSec = 10
)

func poolBudgetBurstSec() int64 {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_POOL_BUDGET_BURST_SEC"))
	if v == "" {
		return defaultPoolBudgetBurstSec
	}
	x, err := strconv.ParseInt(v, 10, 64)
	if err != nil || x < 0 || x > 86400 {
		return defaultPoolBudgetBurstSec
	}
	return x
}

// poolBudgetUnlocked is the part of the day budget available at now (paced + burst).
func poolBudgetUnlocked(budget float64, now int64) float64 {
	if budget <= 0 {
		return 0
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	frac := float64(now%86400+poolBudgetBurstSec()) / 86400.0
	if frac > 1 {
		frac = 1
	}
	return budget * frac
}

const poolPayDayTable = `
CREATE TABLE IF NOT EXISTS pool_pay_day (
  day_id INTEGER PRIMARY KEY,
  accrued_hmc REAL NOT NULL DEFAULT 0,
  clip_events INTEGER NOT NULL DEFAULT 0,
  updated_at INTEGER NOT NULL DEFAULT 0
)`

// Pacing segments (reward changes mid-day: v2 activation 10×, halvings ½). Unlocked budget is
// the emission actually scheduled so far — not "today's fraction × the NEW daily rate", which
// opened ~140 HMC of headroom at once on activation day and underpaid the rest of a halving day.
var poolPayDaySegCols = []string{
	`ALTER TABLE pool_pay_day ADD COLUMN seg_start_unix INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE pool_pay_day ADD COLUMN seg_rate_hmc_s REAL NOT NULL DEFAULT 0`,
	`ALTER TABLE pool_pay_day ADD COLUMN unlocked_base_hmc REAL NOT NULL DEFAULT 0`,
}

// poolBudgetSegUnlocked: unlocked budget at now for a day with base (unlocked before segStart)
// and the current segment rate, plus the burst allowance, capped at the day's total schedule.
func poolBudgetSegUnlocked(base, rate float64, segStart, now int64) float64 {
	if rate <= 0 {
		return base
	}
	dayEnd := (now/86400 + 1) * 86400
	if segStart <= 0 {
		segStart = (now / 86400) * 86400
	}
	if segStart > now {
		// Clock stepped back after a restart: the base already covers up to segStart.
		segStart = now
	}
	u := base + rate*float64(now-segStart+poolBudgetBurstSec())
	if dayTotal := base + rate*float64(dayEnd-segStart); u > dayTotal {
		u = dayTotal
	}
	return u
}

// syncPoolPaySegmentLocked starts a new pacing segment when the daily budget (reward) changed.
// Caller holds m.mu and has run resetPoolPayDayIfNeeded.
func (m *workManager) syncPoolPaySegmentLocked(budget float64, now int64) {
	rate := budget / 86400.0
	dayStart := (now / 86400) * 86400
	if m.poolPaySegStart < dayStart || m.poolPaySegRate <= 0 {
		// New day or no segment yet: the whole day so far at the current rate (old behaviour).
		m.poolPaySegStart = dayStart
		m.poolPaySegRate = rate
		m.poolPayUnlockedBase = 0
		return
	}
	if rate != m.poolPaySegRate {
		m.poolPayUnlockedBase += m.poolPaySegRate * float64(now-m.poolPaySegStart)
		m.poolPaySegStart = now
		m.poolPaySegRate = rate
		m.persistPoolPayDayLocked(now)
	}
}

func (m *workManager) poolBudgetUnlockedLocked(budget float64, now int64) float64 {
	m.syncPoolPaySegmentLocked(budget, now)
	return poolBudgetSegUnlocked(m.poolPayUnlockedBase, m.poolPaySegRate, m.poolPaySegStart, now)
}

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
	for _, q := range poolPayDaySegCols {
		_, _ = db.Exec(q) // duplicate column on re-run is expected
	}
}

func (m *workManager) loadPoolPayDay() {
	if m == nil || m.dedupDB == nil {
		return
	}
	ensurePoolPayDayTable(m.dedupDB)
	day := utcDayID(nowUnix())
	var accrued, segRate, unlockedBase float64
	var clips uint64
	var segStart int64
	err := m.dedupDB.QueryRow(`SELECT accrued_hmc, clip_events, seg_start_unix, seg_rate_hmc_s, unlocked_base_hmc FROM pool_pay_day WHERE day_id=?`, day).
		Scan(&accrued, &clips, &segStart, &segRate, &unlockedBase)
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
	m.poolPaySegStart, m.poolPaySegRate, m.poolPayUnlockedBase = segStart, segRate, unlockedBase
	// Until node metrics load, price shares at the last reward this coordinator saw today
	// (not the compiled 0.01 placeholder: after the v2 switch that underpaid 10×).
	if segRate > 0 && strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC")) == "" {
		if b := math.Round(segRate*poolBlockSec()*1e8) / 1e8; b > 0 {
			m.baseRewardHMC = b
		}
	}
}

func (m *workManager) persistPoolPayDayLocked(now int64) {
	if m == nil || m.dedupDB == nil {
		return
	}
	// Snapshot under caller's m.mu, write off-lock (submit hot path).
	dayID := m.poolPayDayID
	accrued := m.poolPayAccruedDay
	clips := m.poolBudgetClipped
	segStart := m.poolPaySegStart
	segRate := m.poolPaySegRate
	unlocked := m.poolPayUnlockedBase
	db := m.dedupDB
	if m.poolPayPersistWG == nil {
		m.poolPayPersistWG = &sync.WaitGroup{}
	}
	m.poolPayPersistWG.Add(1)
	go func() {
		defer m.poolPayPersistWG.Done()
		ensurePoolPayDayTable(db)
		_, err := db.Exec(`
INSERT INTO pool_pay_day (day_id, accrued_hmc, clip_events, updated_at, seg_start_unix, seg_rate_hmc_s, unlocked_base_hmc) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(day_id) DO UPDATE SET
  accrued_hmc=excluded.accrued_hmc,
  clip_events=excluded.clip_events,
  updated_at=excluded.updated_at,
  seg_start_unix=excluded.seg_start_unix,
  seg_rate_hmc_s=excluded.seg_rate_hmc_s,
  unlocked_base_hmc=excluded.unlocked_base_hmc`,
			dayID, accrued, clips, now, segStart, segRate, unlocked)
		if err != nil {
			log.Printf("pool_pay_day persist: %v", err)
		}
	}()
}

// waitPoolPayDayPersisted blocks until in-flight pool_pay_day writes finish (tests/shutdown).
func (m *workManager) waitPoolPayDayPersisted() {
	if m == nil || m.poolPayPersistWG == nil {
		return
	}
	m.poolPayPersistWG.Wait()
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
	remain := m.poolBudgetUnlockedLocked(budget, now) - m.poolPayAccruedDay
	if remain <= 0 {
		m.poolBudgetClipped++
		if now-m.poolPayPersistUnix >= poolPayPersistGapSec {
			m.poolPayPersistUnix = now
			m.persistPoolPayDayLocked(now)
		}
		return 0
	}
	if payout > remain {
		payout = remain
		m.poolBudgetClipped++
	}
	m.poolPayAccruedDay += payout
	if now-m.poolPayPersistUnix >= poolPayPersistGapSec {
		m.poolPayPersistUnix = now
		m.persistPoolPayDayLocked(now)
	}
	return payout
}

func (m *workManager) poolBudgetStats() map[string]any {
	if m == nil {
		return map[string]any{"enabled": false}
	}
	budget := m.poolDailyBudgetHMC()
	now := nowUnix()
	remain := budget - m.poolPayAccruedDay
	if remain < 0 {
		remain = 0
	}
	m.resetPoolPayDayIfNeeded(now)
	unlockedRemain := m.poolBudgetUnlockedLocked(budget, now) - m.poolPayAccruedDay
	if unlockedRemain < 0 {
		unlockedRemain = 0
	}
	return map[string]any{
		"enabled":             budget > 0,
		"daily_budget_hmc":    budget,
		"accrued_today_hmc":   m.poolPayAccruedDay,
		"remaining_today_hmc": remain,
		"available_now_hmc":   unlockedRemain,
		"burst_sec":           poolBudgetBurstSec(),
		"budget_clip_events":  m.poolBudgetClipped,
		"block_sec":           poolBlockSec(),
		"persisted":           m.dedupDB != nil,
		"note":                "accrual capped to chain emission schedule; settle still requires treasury float",
	}
}

func nowUnix() int64 { return time.Now().Unix() }
