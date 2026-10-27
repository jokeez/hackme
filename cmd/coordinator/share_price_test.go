package main

import (
	"testing"

	"hackme/internal/chain"
)

func TestObserveSharePriceEMAPerHash(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_SHARE_PRICE_EMA", "1")
	t.Setenv("HACKME_COORDINATOR_SHARE_PRICE_EMA_ALPHA", "0.5")
	m := &workManager{}
	base := 0.01
	smSmall, smLarge := uint64(1_000), uint64(100_000)
	tm := uint64(10_000_000)
	pay1 := m.observeSharePrice(base, 1, tm, smSmall)
	want1 := rewardPerShareUnits(base, smSmall, tm)
	if pay1 != want1 {
		t.Fatalf("seed pay=%d want %d", pay1, want1)
	}
	if m.sharePrice == nil || m.sharePrice.emaPerHashHMC <= 0 {
		t.Fatal("ema per-hash not set")
	}
	pay2 := m.observeSharePrice(base, 1, tm, smLarge)
	want2 := chain.HMCToUnits(m.sharePrice.emaPerHashHMC * float64(smLarge))
	if pay2 != want2 {
		t.Fatalf("large-lease pay=%d want %d", pay2, want2)
	}
	t.Setenv("HACKME_COORDINATOR_SHARE_PRICE_EMA", "0")
	pay3 := m.observeSharePrice(base, 1, tm, smLarge)
	if pay3 != rewardPerShareUnits(base, smLarge, tm) {
		t.Fatalf("instant-only pay=%d", pay3)
	}
}

func TestRewardPerShareUnitsMatchesFloat(t *testing.T) {
	base := 0.01
	sm, tm := uint64(1024), uint64(1<<20)
	u := rewardPerShareUnits(base, sm, tm)
	want := chain.HMCToUnits(rewardPerShareAt(base, sm, tm))
	if u != want || u == 0 {
		t.Fatalf("units=%d want=%d", u, want)
	}
}
