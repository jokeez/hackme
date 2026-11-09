package main

import (
	"math"
	"testing"
	"time"
)

func TestDeliveredPoolGHFromShareWork(t *testing.T) {
	m := &workManager{}
	const t0 = int64(1_000_000)
	if _, ok := m.deliveredPoolGHLocked(t0); ok {
		t.Fatal("empty history must not report delivered GH/s")
	}
	// 5 GH/s delivered: 50e9 expected hashes every 10s via shares of share_mod 1e9.
	for i := int64(0); i <= 60; i++ {
		m.noteShareWorkLocked(50, 1_000_000_000, t0+i*10)
	}
	gh, ok := m.deliveredPoolGHLocked(t0 + 600)
	if !ok || math.Abs(gh-5) > 0.01 {
		t.Fatalf("delivered=%v ok=%v want ~5 GH/s", gh, ok)
	}
	if n := len(m.shareWorkSamples); n > int(deliveredGHWindowSec/deliveredSampleEverySec)+2 {
		t.Fatalf("samples not trimmed to window: %d", n)
	}
}

func TestDeliveredPoolGHNeedsMinSpan(t *testing.T) {
	m := &workManager{}
	m.noteShareWorkLocked(10, 1_000_000_000, 100)
	m.noteShareWorkLocked(10, 1_000_000_000, 150)
	if _, ok := m.deliveredPoolGHLocked(150 + deliveredGHMinSpanSec - 51); ok {
		t.Fatal("span below minimum must fall back to device GH/s")
	}
}

func TestDeliveredPoolGHQuietPoolDecays(t *testing.T) {
	m := &workManager{}
	for i := int64(0); i <= 60; i++ {
		m.noteShareWorkLocked(50, 1_000_000_000, i*10)
	}
	// No submits for 20 minutes: the rate over the true span must fall, not freeze at 5 GH/s.
	gh, ok := m.deliveredPoolGHLocked(600 + 1200)
	if !ok || gh >= 2 {
		t.Fatalf("quiet pool delivered=%v ok=%v, want well below 5", gh, ok)
	}
}

func TestEnrichPoolStatsPrefersDeliveredOverDeviceGH(t *testing.T) {
	now := time.Now().Unix()
	wm := &workManager{
		worker: map[string]workerPayoutStat{
			// Device-busy GH/s claims 15 GH/s, like a GPU rig idling on RTT between short leases.
			"rig-gpu": {LastHashrateGHS: 15, LastSeenUnix: now, LastPoHSeenUnix: now},
		},
	}
	for i := int64(-600); i <= 0; i += 10 {
		wm.noteShareWorkLocked(30, 1_000_000_000, now+i) // 3 GH/s actually delivered
	}
	out := map[string]any{}
	enrichPoolStatsForPublic(out, nil, wm)
	gh, _ := out["pool_hashrate_gh_s"].(float64)
	if gh < 14 {
		t.Fatalf("public pool_hashrate_gh_s=%v want board/device ~15", gh)
	}
	if out["pool_hashrate_kind"] != "board_device_sum" {
		t.Fatalf("kind=%v want board_device_sum", out["pool_hashrate_kind"])
	}
	if del, _ := out["pool_hashrate_gh_s_delivered"].(float64); math.Abs(del-3) > 0.2 {
		t.Fatalf("delivered side-channel=%v want ~3", del)
	}
	if dev, _ := out["pool_hashrate_gh_s_device"].(float64); dev < 14 {
		t.Fatalf("device GH/s must stay visible separately, got %v", dev)
	}
}

// Solvency invariant: with clamped M from poolLoadTargetM, full-fleet pay per day (GH·1e9·86400·base/M)
// never exceeds daily emission base·86400/block_sec — independent of fleet size. PR #37 (M from
// GH·modMin) broke this by ~15,000× and only the daily budget kept the pool solvent.
func TestPoolLoadTargetMPayNeverExceedsEmission(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_BLOCK_SEC", "30")
	t.Setenv("HACKME_COORDINATOR_POOL_LOAD_SOLVENCY", "")
	const base = 0.01
	emission := base * 86400 / 30
	wm := &workManager{}
	for _, gh := range []float64{0.05, 0.5, 3, 15, 120, 2000, 20000} {
		for _, miners := range []int{1, 28, 1000} {
			M := wm.clampTargetMod(poolLoadTargetM(gh, miners, poolTargetModMin, true))
			if M == 0 {
				t.Fatalf("gh=%v miners=%d: zero M", gh, miners)
			}
			pay := gh * 1e9 * 86400 * base / float64(M)
			if pay > emission*1.001 {
				t.Fatalf("gh=%v miners=%d M=%d pays %.2f HMC/day > emission %.2f", gh, miners, M, pay, emission)
			}
		}
	}
}

// Economic invariant over lease shapes (model test, no hashing): one day of pool work through
// the production formulas — M from poolLoadTargetM, per-share price base·sm/M, per-lease ceiling,
// SUP from credited work — must pay ≤ daily emission in HMC and ≤ 12% of that HMC in SUP, for
// any lease size (2^22…2^36) and fleet. HMC must not scale with lease shape; the SUP side guards
// the daily cap (the attempt-based SUP basis itself is pinned by
// TestSUPFollowsVerifiedSharePayNotClaimedAttempts).
func TestEconomicInvariantAcrossLeaseShapes(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_POOL_BLOCK_SEC", "30")
	t.Setenv("HACKME_COORDINATOR_POOL_LOAD_SOLVENCY", "")
	const base = 0.01
	emission := base * 86400 / 30
	for _, gh := range []float64{0.5, 6, 30} {
		{
			for _, batch := range []uint64{1 << 22, 1 << 28, 1 << 32, 1 << 36} {
				wm := &workManager{
					baseRewardHMC: base, shareDivisor: 1024, shareStrict: true, rewardPerM: 0.00005,
					defaultBatch: 1 << 22, maxClaimBatch: 1 << 36,
					supPolicy: supPolicy{Enabled: true, RateOfHMC: 0.08, DailyCapRatio: 0.12, StreakSec: 72 * 3600, StreakBonus: 0.25, StaleRateMax: 0.05, ReducedMult: 0.25},
					supMeta:   make(map[string]workerSupMeta),
					abuse:     make(map[string]workerAbuseState),
				}
				wm.targetMod = wm.clampTargetMod(poolLoadTargetM(gh, 26, poolTargetModMin, true))
				sm := shareModForLease(wm.targetMod, wm.shareDivisor, batch)
				hashesPerDay := gh * 1e9 * 86400
				leases := hashesPerDay / float64(batch)
				expShares := float64(batch) / float64(sm)
				payPerLease := expShares * rewardPerShareAt(base, sm, wm.targetMod)
				if ceil := wm.maxSubmitHMCForBatch(batch); payPerLease > ceil {
					payPerLease = ceil
				}
				hmcDay := payPerLease * leases
				// SUP: feed the day through computeSUPAccrual in 1000 equal steps of credited work.
				now := time.Now().Unix()
				var supDay float64
				for i := 0; i < 1000; i++ {
					supDay += wm.computeSUPAccrual("w", hmcDay/1000, true, now)
				}
				if hmcDay > emission*1.001 {
					t.Fatalf("gh=%v batch=2^%d: HMC/day %.3f > emission %.2f", gh, bitsOf(batch), hmcDay, emission)
				}
				if supDay > hmcDay*0.12*1.0001 {
					t.Fatalf("gh=%v batch=2^%d: SUP/day %.4f > 12%% of HMC/day %.4f", gh, bitsOf(batch), supDay, hmcDay)
				}
			}
		}
	}
}

func bitsOf(x uint64) int {
	n := 0
	for x > 1 {
		x >>= 1
		n++
	}
	return n
}
