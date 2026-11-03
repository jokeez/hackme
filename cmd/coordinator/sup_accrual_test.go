package main

import (
	"testing"
	"time"
)

func testWorkManagerForSUP() *workManager {
	return &workManager{
		rewardPerM: 1.0,
		supPolicy: supPolicy{
			Enabled:       true,
			RateOfHMC:     0.08,
			StreakSec:     72 * 3600,
			StreakBonus:   0.25,
			DailyCapRatio: 0.12,
			RequireHybrid: true,
			StaleRateMax:  0.05,
			ReducedMult:   0.25,
		},
		supMeta: make(map[string]workerSupMeta),
		abuse:   make(map[string]workerAbuseState),
	}
}

func TestSUPAccrualRequiresHybrid(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	if got := wm.computeSUPAccrual("w1", 1.0, false, now); got != 0 {
		t.Fatalf("unsigned/hybrid=false want 0 SUP, got %v", got)
	}
	got := wm.computeSUPAccrual("w1", 1.0, true, now)
	if got <= 0 {
		t.Fatalf("hybrid signed want positive SUP, got %v", got)
	}
}

func TestSUPAccrualZeroWhenBanned(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	wm.abuse["w1"] = workerAbuseState{BannedUntil: now + 3600}
	if got := wm.computeSUPAccrual("w1", 1.0, true, now); got != 0 {
		t.Fatalf("banned want 0 SUP, got %v", got)
	}
}

func TestSUPAccrualDailyCap(t *testing.T) {
	wm := testWorkManagerForSUP()
	wm.supPolicy.DailyCapRatio = 0.01
	now := time.Now().Unix()
	var total float64
	for i := 0; i < 50; i++ {
		total += wm.computeSUPAccrual("w1", 1.0, true, now)
	}
	cap := wm.hmcAccruedDay * wm.supPolicy.DailyCapRatio
	if total > cap+1e-9 {
		t.Fatalf("daily cap exceeded: accrued %v cap %v hmc_day %v", total, cap, wm.hmcAccruedDay)
	}
}

func TestSUPAccrualZeroWithoutVerifiedWork(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	if sup := wm.computeSUPAccrual("w1", 0, true, now); sup != 0 {
		t.Fatalf("no verified-work HMC must accrue no SUP, got %v", sup)
	}
}

func TestSUPStaleBreaksCleanStreak(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	_ = wm.computeSUPAccrual("w1", 1.0, true, now)
	if wm.supMeta["w1"].CleanSinceUnix == 0 {
		t.Fatal("expected clean streak armed on accept")
	}
	wm.noteWorkerStale("w1", now+10)
	if wm.supMeta["w1"].CleanSinceUnix != 0 {
		t.Fatalf("stale must reset CleanSinceUnix, got %d", wm.supMeta["w1"].CleanSinceUnix)
	}
}

func TestSUPDailyCapUsesWorkHMC(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	_ = wm.computeSUPAccrual("w1", 1.0, true, now)
	if wm.hmcAccruedDay < 0.99 || wm.hmcAccruedDay > 1.01 {
		t.Fatalf("hmc_accrued_day should be the verified-work HMC (~1), got %v", wm.hmcAccruedDay)
	}
}

// Economics v2: pool SUP is a share of chain SUP, never a second emission. With chain
// 0.002 SUP / 0.1 HMC per block the pool pays ≤ 2% of credited HMC in SUP — streak bonus off.
func TestSUPAccrualV2FollowsChainSUPPerBlock(t *testing.T) {
	wm := &workManager{
		supPolicy:     supPolicy{Enabled: true, RateOfHMC: 0.08, DailyCapRatio: 0.12, StreakSec: 3600, StreakBonus: 0.25, StaleRateMax: 0.05, ReducedMult: 0.25},
		supMeta:       map[string]workerSupMeta{"w": {CleanSinceUnix: 1}}, // long clean streak
		abuse:         map[string]workerAbuseState{},
		baseRewardHMC: 0.1,
		supPerBlock:   0.002,
	}
	now := int64(1_800_000_000)
	var hmc, sup float64
	for i := 0; i < 2880; i++ { // one day of per-block pay
		hmc += 0.1
		sup += wm.computeSUPAccrual("w", 0.1, true, now+int64(i))
	}
	if want := hmc * 0.02; sup > want*1.000001 || sup < want*0.999 {
		t.Fatalf("v2 SUP/day %.6f want %.6f (= chain 0.002/block × 2880)", sup, want)
	}
	// Before activation the chain reports 0 SUP/block: legacy policy applies.
	wm2 := &workManager{supPolicy: wm.supPolicy, supMeta: map[string]workerSupMeta{}, abuse: map[string]workerAbuseState{}, baseRewardHMC: 0.01}
	if got := wm2.computeSUPAccrual("w", 1, true, now); got < 0.079 || got > 0.081 {
		t.Fatalf("v1 SUP for 1 HMC = %v want 0.08", got)
	}
}
