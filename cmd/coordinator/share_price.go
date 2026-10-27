package main

import (
	"database/sql"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"hackme/internal/chain"
)

// Windowed EMA of per-hash price (HMC/hash), not per-share.
// Pay for one share = HMCToUnits(ema_per_hash_hmc × share_mod).
// Mixing raw share prices across leases with different share_mod starved large GPU leases.

const (
	defaultSharePriceEMAAlpha  = 0.25
	sharePriceJournalMinGapSec = int64(5)
)

type sharePriceState struct {
	emaPerHashHMC    float64
	lastInstantShare uint64
	samples          uint64
	sharesPaid       uint64
	lastJournalUnix  int64
	windowTargetMod  uint64
	windowShareMod   uint64
}

func envSharePriceEMA() bool {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SHARE_PRICE_EMA"))
	if v == "" {
		return true
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}

func envSharePriceEMAAlpha() float64 {
	v := strings.TrimSpace(os.Getenv("HACKME_COORDINATOR_SHARE_PRICE_EMA_ALPHA"))
	if v == "" {
		return defaultSharePriceEMAAlpha
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil || x <= 0 || x > 1 {
		return defaultSharePriceEMAAlpha
	}
	return x
}

func rewardPerShareUnits(baseRewardHMC float64, shareMod, targetMod uint64) uint64 {
	return chain.HMCToUnits(rewardPerShareAt(baseRewardHMC, shareMod, targetMod))
}

func rewardPerHashHMC(baseRewardHMC float64, targetMod uint64) float64 {
	if baseRewardHMC <= 0 || targetMod == 0 {
		return 0
	}
	return baseRewardHMC / float64(targetMod)
}

// observeSharePrice EMAs per-hash HMC and returns pay units for one share.
func (m *workManager) observeSharePrice(baseRewardHMC float64, nShares, targetMod, shareMod uint64) uint64 {
	instantShare := rewardPerShareUnits(baseRewardHMC, shareMod, targetMod)
	if m == nil || instantShare == 0 || shareMod == 0 || targetMod == 0 {
		return instantShare
	}
	perHash := rewardPerHashHMC(baseRewardHMC, targetMod)
	if perHash <= 0 {
		return instantShare
	}
	if m.sharePrice == nil {
		m.sharePrice = &sharePriceState{}
	}
	st := m.sharePrice
	st.lastInstantShare = instantShare
	st.windowTargetMod = targetMod
	st.windowShareMod = shareMod
	st.sharesPaid += nShares
	st.samples++
	if st.emaPerHashHMC <= 0 {
		st.emaPerHashHMC = perHash
	} else {
		alpha := envSharePriceEMAAlpha()
		st.emaPerHashHMC = alpha*perHash + (1-alpha)*st.emaPerHashHMC
		if st.emaPerHashHMC <= 0 {
			st.emaPerHashHMC = perHash
		}
	}
	pay := instantShare
	if envSharePriceEMA() && st.emaPerHashHMC > 0 {
		pay = chain.HMCToUnits(st.emaPerHashHMC * float64(shareMod))
		if pay == 0 {
			pay = instantShare
		}
	}
	m.maybeJournalSharePrice(nShares, instantShare, pay, targetMod, shareMod)
	return pay
}

func (m *workManager) maybeJournalSharePrice(nShares, instant, ema, targetMod, shareMod uint64) {
	if m == nil || m.dedupDB == nil {
		return
	}
	now := time.Now().Unix()
	if m.sharePrice != nil && now-m.sharePrice.lastJournalUnix < sharePriceJournalMinGapSec {
		return
	}
	if m.sharePrice != nil {
		m.sharePrice.lastJournalUnix = now
	}
	ensureSharePriceJournal(m.dedupDB)
	_, err := m.dedupDB.Exec(`
INSERT INTO share_price_journal (created_at, share_count, instant_units, ema_units, target_mod, share_mod)
VALUES (?, ?, ?, ?, ?, ?)`, now, nShares, instant, ema, targetMod, shareMod)
	if err != nil {
		log.Printf("share_price_journal: %v", err)
	}
}

const sharePriceJournalTable = `
CREATE TABLE IF NOT EXISTS share_price_journal (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at INTEGER NOT NULL,
  share_count INTEGER NOT NULL DEFAULT 0,
  instant_units INTEGER NOT NULL DEFAULT 0,
  ema_units INTEGER NOT NULL DEFAULT 0,
  target_mod INTEGER NOT NULL DEFAULT 0,
  share_mod INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_share_price_journal_ts ON share_price_journal(created_at);
`

func ensureSharePriceJournal(db *sql.DB) {
	if db == nil {
		return
	}
	_, _ = db.Exec(sharePriceJournalTable)
}
