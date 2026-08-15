package main

import (
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// workerSupMeta tracks clean-mining streaks for SUP quality multiplier.
type workerSupMeta struct {
	CleanSinceUnix int64  `json:"clean_since_unix,omitempty"`
	RollingAccepts uint64 `json:"rolling_accepts,omitempty"`
	RollingStale   uint64 `json:"rolling_stale,omitempty"`
}

type supPolicy struct {
	Enabled       bool
	RateOfHMC     float64 // base SUP = attempt-only HMC slice * rate
	StreakSec     int64
	StreakBonus   float64 // added to multiplier (e.g. 0.25 -> 1.25x)
	DailyCapRatio float64 // max SUP accrual per UTC day <= ratio * HMC accrued that day
	RequireHybrid bool
	StaleRateMax  float64 // above this accept/(accept+stale) -> 0.25x
	ReducedMult   float64 // multiplier when stale rate high
}

func supOnChainSettleEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_SUP_ON_CHAIN_SETTLE")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
}

func supPolicyFromEnv() supPolicy {
	p := supPolicy{
		Enabled:       true,
		RateOfHMC:     0.08,
		StreakSec:     72 * 3600,
		StreakBonus:   0.25,
		DailyCapRatio: 0.12,
		RequireHybrid: true,
		StaleRateMax:  0.05,
		ReducedMult:   0.25,
	}
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_COORDINATOR_SUP_ENABLED"))); v != "" {
		p.Enabled = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SUP_RATE_OF_HMC")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x >= 0 && x <= 1 {
			p.RateOfHMC = x
		}
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SUP_STREAK_SEC")); v != "" {
		if x, err := strconv.ParseInt(v, 10, 64); err == nil && x >= 3600 {
			p.StreakSec = x
		}
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SUP_STREAK_BONUS")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x >= 0 && x <= 1 {
			p.StreakBonus = x
		}
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SUP_DAILY_CAP_RATIO")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x >= 0 && x <= 1 {
			p.DailyCapRatio = x
		}
	}
	if v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_COORDINATOR_SUP_REQUIRE_HYBRID"))); v != "" {
		p.RequireHybrid = v == "1" || v == "true" || v == "yes" || v == "on"
	}
	if v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SUP_STALE_RATE_MAX")); v != "" {
		if x, err := strconv.ParseFloat(v, 64); err == nil && x >= 0 && x <= 1 {
			p.StaleRateMax = x
		}
	}
	return p
}

func utcDayID(unix int64) int64 {
	if unix <= 0 {
		unix = time.Now().Unix()
	}
	return unix / 86400
}

func (m *workManager) resetSupDayIfNeeded(now int64) {
	day := utcDayID(now)
	if m.supDayID != day {
		m.supDayID = day
		m.supAccruedDay = 0
		m.hmcAccruedDay = 0
	}
}

// noteWorkerStale records a stale submit attempt for SUP quality (caller holds m.mu).
func (m *workManager) noteWorkerStale(workerID string, now int64) {
	if workerID == "" || m.supMeta == nil {
		return
	}
	meta := m.supMeta[workerID]
	meta.RollingStale++
	// V3-M1: stale breaks the clean streak (do not arm CleanSinceUnix here).
	meta.CleanSinceUnix = 0
	m.supMeta[workerID] = meta
}

func (m *workManager) supQualityMult(workerID string, now int64) float64 {
	if workerID == "" {
		return 0
	}
	if st, ok := m.abuse[workerID]; ok {
		if st.BannedUntil > now || st.BadStrikes > 0 {
			return 0
		}
	}
	meta := m.supMeta[workerID]
	accepts := meta.RollingAccepts
	stale := meta.RollingStale
	total := accepts + stale
	if total > 2000 {
		accepts = accepts * 2000 / total
		stale = stale * 2000 / total
		total = 2000
	}
	mult := 1.0
	if total > 20 {
		staleRate := float64(stale) / float64(total)
		if staleRate > m.supPolicy.StaleRateMax {
			mult = m.supPolicy.ReducedMult
		}
	}
	if meta.CleanSinceUnix > 0 && now-meta.CleanSinceUnix >= m.supPolicy.StreakSec {
		mult += m.supPolicy.StreakBonus
	}
	if mult > 1.5 {
		mult = 1.5
	}
	return mult
}

// computeSUPAccrual returns SUP to add for an accepted submit (caller holds m.mu).
func (m *workManager) computeSUPAccrual(workerID string, hmcPayout float64, paidAttempts uint64, hybridSigned bool, now int64) float64 {
	if m == nil || !m.supPolicy.Enabled || workerID == "" {
		return 0
	}
	if paidAttempts == 0 || hmcPayout <= 0 {
		return 0
	}
	if m.supPolicy.RequireHybrid && !hybridSigned {
		return 0
	}
	if st, ok := m.abuse[workerID]; ok && st.BannedUntil > now {
		return 0
	}
	attemptHMC := (float64(paidAttempts) / 1_000_000.0) * m.rewardPerM
	if attemptHMC <= 0 {
		return 0
	}
	mult := m.supQualityMult(workerID, now)
	if mult <= 0 {
		return 0
	}
	sup := attemptHMC * m.supPolicy.RateOfHMC * mult
	if sup <= 0 || math.IsNaN(sup) || math.IsInf(sup, 0) {
		return 0
	}
	m.resetSupDayIfNeeded(now)
	// V3-M3: daily SUP cap basis is attempt-HMC only (not found_bonus).
	m.hmcAccruedDay += attemptHMC
	if m.supPolicy.DailyCapRatio > 0 && m.hmcAccruedDay > 0 {
		cap := m.hmcAccruedDay * m.supPolicy.DailyCapRatio
		if m.supAccruedDay >= cap {
			return 0
		}
		if m.supAccruedDay+sup > cap {
			sup = cap - m.supAccruedDay
		}
	}
	if sup <= 0 {
		return 0
	}
	m.supAccruedDay += sup
	meta := m.supMeta[workerID]
	meta.RollingAccepts++
	if meta.CleanSinceUnix == 0 {
		meta.CleanSinceUnix = now
	}
	m.supMeta[workerID] = meta
	return sup
}

func (m *workManager) supPolicyStats() map[string]any {
	if m == nil {
		return map[string]any{"enabled": false}
	}
	return map[string]any{
		"enabled":           m.supPolicy.Enabled,
		"rate_of_hmc":       m.supPolicy.RateOfHMC,
		"streak_sec":        m.supPolicy.StreakSec,
		"streak_bonus":      m.supPolicy.StreakBonus,
		"daily_cap_ratio":   m.supPolicy.DailyCapRatio,
		"require_hybrid":    m.supPolicy.RequireHybrid,
		"stale_rate_max":    m.supPolicy.StaleRateMax,
		"total_payout_sup":  m.totalPayoutSUP,
		"sup_accrued_today": m.supAccruedDay,
		"hmc_accrued_today": m.hmcAccruedDay,
		"on_chain_settle":   supOnChainSettleEnabled(),
		"listing_note":      "SUP list on exchanges after primary HMC listing; accrual is live in coordinator",
	}
}
