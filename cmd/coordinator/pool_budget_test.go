package main

import (
	"testing"
	"time"
)

func TestApplyPoolDailyBudgetCapsToEmission(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC", "1.0")
	t.Setenv("HACKME_COORDINATOR_POOL_BUDGET_BURST_SEC", "86400") // whole day unlocked: hard cap only
	t.Setenv("HACKME_COORDINATOR_POOL_BLOCK_SEC", "30")
	wm := &workManager{baseRewardHMC: 0.01}
	now := time.Now().Unix()
	got := wm.applyPoolDailyBudget(0.4, now)
	if got != 0.4 {
		t.Fatalf("first credit want 0.4 got %v", got)
	}
	got = wm.applyPoolDailyBudget(0.5, now)
	if got != 0.5 {
		t.Fatalf("second credit want 0.5 got %v", got)
	}
	got = wm.applyPoolDailyBudget(0.2, now)
	if got < 0.099 || got > 0.101 {
		t.Fatalf("third credit want ~0.1 remain, got %v", got)
	}
	got = wm.applyPoolDailyBudget(0.5, now)
	if got != 0 {
		t.Fatalf("over budget want 0 got %v", got)
	}
	st := wm.poolBudgetStats()
	if st["accrued_today_hmc"].(float64) < 0.99 || st["accrued_today_hmc"].(float64) > 1.01 {
		t.Fatalf("accrued=%v", st["accrued_today_hmc"])
	}
}

// Pacing: a big fleet at the start of the UTC day can take only the unlocked share (elapsed +
// burst), so miners later in the day still accrue. Before pacing the day drained by ~07:30 UTC.
func TestPoolDailyBudgetIsPacedOverUTCDay(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC", "28.8")
	t.Setenv("HACKME_COORDINATOR_POOL_BUDGET_BURST_SEC", "3600")
	wm := &workManager{baseRewardHMC: 0.01}
	day := int64(20760) * 86400
	at := func(sec int64) int64 { return day + sec }
	// 00:30 UTC: unlocked = 28.8 · (1800+3600)/86400 = 1.8
	got := 0.0
	for i := 0; i < 1000; i++ {
		got += wm.applyPoolDailyBudget(0.01, at(1800))
	}
	if got < 1.79 || got > 1.801 {
		t.Fatalf("00:30 burst sniper took %.4f, want ≈1.8 (paced)", got)
	}
	if wm.applyPoolDailyBudget(0.01, at(1800)) != 0 {
		t.Fatal("over the paced line must clip")
	}
	// 12:00 UTC: budget kept unlocking for later miners.
	late := 0.0
	for i := 0; i < 2000; i++ {
		late += wm.applyPoolDailyBudget(0.01, at(12*3600))
	}
	wantLate := 28.8*(12*3600+3600)/86400 - got
	if late < wantLate-0.02 || late > wantLate+0.02 {
		t.Fatalf("12:00 accrual %.3f want ≈%.3f", late, wantLate)
	}
	// 23:59 UTC: never above the daily budget.
	for i := 0; i < 5000; i++ {
		wm.applyPoolDailyBudget(0.01, at(86399))
	}
	if wm.poolPayAccruedDay > 28.8+1e-9 {
		t.Fatalf("day accrued %.4f > budget", wm.poolPayAccruedDay)
	}
}

func TestPoolBudgetBurstSecEnv(t *testing.T) {
	for _, c := range []struct {
		env  string
		want int64
	}{{"", 3600}, {"0", 0}, {"86400", 86400}, {"-1", 3600}, {"90000", 3600}, {"x", 3600}} {
		t.Setenv("HACKME_COORDINATOR_POOL_BUDGET_BURST_SEC", c.env)
		if got := poolBudgetBurstSec(); got != c.want {
			t.Fatalf("env %q: got %d want %d", c.env, got, c.want)
		}
	}
}

func TestPoolDailyBudgetDefaultFromEmission(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC", "")
	t.Setenv("HACKME_COORDINATOR_POOL_BLOCK_SEC", "30")
	wm := &workManager{baseRewardHMC: 0.01}
	got := wm.poolDailyBudgetHMC()
	want := 0.01 * (86400.0 / 30.0) // 28.8
	if got < want-0.01 || got > want+0.01 {
		t.Fatalf("budget=%v want ~%v", got, want)
	}
}

func TestLoadRetargetUsesHashTimesBlockSec(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_BLOCK_SEC", "30")
	wm := &workManager{
		poolRetarget:       true,
		targetMod:          poolTargetModMin,
		targetModMin:       poolTargetModMin,
		targetModMax:       poolTargetModMax,
		poolRetargetMinSec: 0,
		baseRewardHMC:      0.01,
		rewardAuto:         true,
	}
	// Ramp catches up over ticks (1.35× when far below load).
	for i := 0; i < 80; i++ {
		wm.lastPoolRetargetUnix = 0
		wm.maybeRetargetPoolLoadLocked(time.Now().Unix()+int64(i), 15.0, 3)
	}
	// 15 GH/s × 1e9 × 30s ≈ 4.5e11 (boost for miner count may raise slightly).
	if wm.targetMod < 200_000_000_000 {
		t.Fatalf("after load retarget M too small: %d (want ~4.5e11 scale)", wm.targetMod)
	}
	if wm.targetMod > poolTargetModMax {
		t.Fatalf("M %d exceeds max", wm.targetMod)
	}
}
