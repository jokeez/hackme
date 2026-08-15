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
	if got := wm.computeSUPAccrual("w1", 1.0, 1_000_000, false, now); got != 0 {
		t.Fatalf("unsigned/hybrid=false want 0 SUP, got %v", got)
	}
	got := wm.computeSUPAccrual("w1", 1.0, 1_000_000, true, now)
	if got <= 0 {
		t.Fatalf("hybrid signed want positive SUP, got %v", got)
	}
}

func TestSUPAccrualZeroWhenBanned(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	wm.abuse["w1"] = workerAbuseState{BannedUntil: now + 3600}
	if got := wm.computeSUPAccrual("w1", 1.0, 1_000_000, true, now); got != 0 {
		t.Fatalf("banned want 0 SUP, got %v", got)
	}
}

func TestSUPAccrualDailyCap(t *testing.T) {
	wm := testWorkManagerForSUP()
	wm.supPolicy.DailyCapRatio = 0.01
	now := time.Now().Unix()
	var total float64
	for i := 0; i < 50; i++ {
		total += wm.computeSUPAccrual("w1", 100.0, 1_000_000, true, now)
	}
	cap := wm.hmcAccruedDay * wm.supPolicy.DailyCapRatio
	if total > cap+1e-9 {
		t.Fatalf("daily cap exceeded: accrued %v cap %v hmc_day %v", total, cap, wm.hmcAccruedDay)
	}
}

func TestSUPAccrualIgnoresFoundBonusSlice(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	// Large HMC payout (found bonus) but tiny paid attempts -> SUP stays small.
	sup := wm.computeSUPAccrual("w1", 50.0, 10_000, true, now)
	if sup > 0.01 {
		t.Fatalf("SUP should track attempt slice not full payout, got %v", sup)
	}
}

func TestSUPStaleBreaksCleanStreak(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	_ = wm.computeSUPAccrual("w1", 1.0, 1_000_000, true, now)
	if wm.supMeta["w1"].CleanSinceUnix == 0 {
		t.Fatal("expected clean streak armed on accept")
	}
	wm.noteWorkerStale("w1", now+10)
	if wm.supMeta["w1"].CleanSinceUnix != 0 {
		t.Fatalf("stale must reset CleanSinceUnix, got %d", wm.supMeta["w1"].CleanSinceUnix)
	}
}

func TestSUPDailyCapUsesAttemptHMCNotFoundBonus(t *testing.T) {
	wm := testWorkManagerForSUP()
	now := time.Now().Unix()
	_ = wm.computeSUPAccrual("w1", 50.0, 1_000_000, true, now) // payout 50, attempt slice = 1.0
	if wm.hmcAccruedDay < 0.99 || wm.hmcAccruedDay > 1.01 {
		t.Fatalf("hmc_accrued_day should be attempt HMC (~1), got %v", wm.hmcAccruedDay)
	}
}
