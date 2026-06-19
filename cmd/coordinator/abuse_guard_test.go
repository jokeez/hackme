package main

import (
	"math"
	"testing"
	"time"

	"hackme/internal/chain"
)

func TestOversizedBatchClaimRejected(t *testing.T) {
	wm := &workManager{
		defaultBatch:    1 << 22,
		maxClaimBatch:   1 << 24,
		targetMod:       1_000_003,
		leaseSec:        30,
		rewardPerM:      0.001,
		maxWorkers:      100,
		maxActiveLeases: 100,
		active:          make(map[workKey]leaseRecord),
		worker:          make(map[string]workerPayoutStat),
		abuse:           make(map[string]workerAbuseState),
	}
	huge := uint64(137_000_000_000)
	_, _, _, _, _, ok, reason := wm.claim("evil", huge)
	if ok || reason != "batch_size_too_large" {
		t.Fatalf("claim huge batch: ok=%v reason=%q", ok, reason)
	}
}

func TestOversizedBatchCannotDrainPool(t *testing.T) {
	const batch = uint64(137_000_000_000)
	wm := &workManager{
		defaultBatch:      1 << 22,
		maxClaimBatch:     1 << 24,
		targetMod:         1_000_003,
		leaseSec:          90,
		rewardPerM:        0.001,
		maxWorkers:        100,
		maxActiveLeases:   100,
		maxDedupEntries:   1000,
		active:            make(map[workKey]leaseRecord),
		worker:            make(map[string]workerPayoutStat),
		abuse:             make(map[string]workerAbuseState),
		ipAbuse:           make(map[string]workerAbuseState),
		acceptedFoundNonces: make(map[uint64]struct{}),
		acceptedResultHashes: make(map[string]struct{}),
	}
	now := time.Now().Unix()
	// Simulate legacy poisoned lease already in memory (pre-patch coordinator).
	wm.active[workKey{base: 1, batch: batch}] = leaseRecord{
		WorkerID:  "legacy-evil",
		BaseNonce: 1,
		BatchSize: batch,
		IssuedAt:  now - 10,
		ExpiresAt: now + 60,
		TargetMod: wm.targetMod,
	}
	found := uint64(1)
	for chain.PohEval(found)%wm.targetMod != 0 {
		found++
	}
	accepted, reason, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "legacy-evil",
		BaseNonce:   1,
		BatchSize:   batch,
		Found:       true,
		FoundNonce:  found,
		ResultHash:  "deadbeef",
		HashrateGHS: 1000,
	})
	if accepted {
		t.Fatalf("expected reject for legacy oversized batch, payout=%v", payout)
	}
	if reason != "batch_size_too_large" {
		t.Fatalf("reason=%q payout=%v", reason, payout)
	}
	st := wm.stats(true)
	total, _ := st["total_payout_hmc"].(float64)
	if total > 0.0001 {
		t.Fatalf("pool drained: total_payout_hmc=%v", total)
	}
}

func TestRevokeWorkerRollsBackPayout(t *testing.T) {
	wm := &workManager{
		defaultBatch: 1000,
		maxClaimBatch: 4000,
		worker: map[string]workerPayoutStat{
			"abuser": {
				PayoutHMC:    20146.35,
				PayoutSUP:    1610.98,
				AcceptedAtt:  125_000_000_000_000,
				LastClientIP: "104.251.226.83",
			},
		},
		totalPayoutHMC: 20207.45,
		totalPayoutSUP: 1612.0,
		totalAttempts:  125_000_000_000_000,
		abuse:          make(map[string]workerAbuseState),
		ipAbuse:        make(map[string]workerAbuseState),
	}
	out := wm.revokeWorker("abuser", "", true)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("revoke: %+v", out)
	}
	if rolled, _ := out["rolled_back_payout_hmc"].(float64); math.Abs(rolled-20146.35) > 0.01 {
		t.Fatalf("rolled_back=%v", rolled)
	}
	if _, exists := wm.worker["abuser"]; exists {
		t.Fatal("abuser row should be deleted")
	}
	if wm.totalPayoutHMC > 100 {
		t.Fatalf("total_payout_hmc=%v", wm.totalPayoutHMC)
	}
	if wm.abuse["abuser"].BannedUntil != coordinatorPermabanUntil {
		t.Fatalf("not permabanned: %+v", wm.abuse["abuser"])
	}
}

func newAbuseTestWM() *workManager {
	return &workManager{
		defaultBatch:         1 << 22,
		maxClaimBatch:        1 << 24,
		targetMod:            1_000_003,
		leaseSec:             30,
		rewardPerM:           0.01,
		foundBonus:           0.01,
		maxWorkers:           100,
		maxActiveLeases:      100,
		maxDedupEntries:      1000,
		active:               make(map[workKey]leaseRecord),
		worker:               make(map[string]workerPayoutStat),
		abuse:                make(map[string]workerAbuseState),
		ipAbuse:              make(map[string]workerAbuseState),
		acceptedFoundNonces:  make(map[uint64]struct{}),
		acceptedResultHashes: make(map[string]struct{}),
	}
}

func TestAttemptsInflationCappedToDefaultBatch(t *testing.T) {
	wm := newAbuseTestWM()
	base, size, _, _, _, ok, _ := wm.claim("w1", wm.defaultBatch)
	if !ok {
		t.Fatal("claim failed")
	}
	_, _, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w1",
		BaseNonce:   base,
		BatchSize:   size,
		Attempts:    999_000_000_000,
		Found:       false,
		HashrateGHS: 25,
	})
	max := (float64(wm.defaultBatch) / 1_000_000.0) * wm.rewardPerM * 1.05
	if payout > max+1e-9 {
		t.Fatalf("payout=%v max=%v", payout, max)
	}
}

func TestFoundWithoutHashratePaysBonusOnly(t *testing.T) {
	wm := newAbuseTestWM()
	base, size, _, _, _, ok, _ := wm.claim("w2", wm.defaultBatch)
	if !ok {
		t.Fatal("claim failed")
	}
	found := uint64(1)
	for chain.PohEval(found)%wm.targetMod != 0 {
		found++
	}
	_, _, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:   "w2",
		BaseNonce:  base,
		BatchSize:  size,
		Attempts:   size,
		Found:      true,
		FoundNonce: found,
		ResultHash: "abc123",
		HashrateGHS: 0,
	})
	if payout > wm.foundBonus+0.001 {
		t.Fatalf("expected ~found_bonus only, got %v", payout)
	}
}

func TestMaxClaimBatchSubmitPaysDefaultBatchNotLease(t *testing.T) {
	wm := newAbuseTestWM()
	lease := wm.maxClaimBatch
	base, size, _, _, _, ok, _ := wm.claim("w3", lease)
	if !ok || size != lease {
		t.Fatalf("claim lease=%d ok=%v size=%d", lease, ok, size)
	}
	_, _, payout, _, _ := wm.submit(submitWorkRequest{
		WorkerID:    "w3",
		BaseNonce:   base,
		BatchSize:   size,
		Attempts:    size,
		Found:       false,
		HashrateGHS: 40,
	})
	max := (float64(wm.defaultBatch) / 1_000_000.0) * wm.rewardPerM * 1.05
	if payout > max+1e-9 {
		t.Fatalf("paid lease-sized accrual: payout=%v max=%v", payout, max)
	}
}

func TestWorkerIDRotationCannotDrainPool(t *testing.T) {
	wm := newAbuseTestWM()
	wm.rewardPerM = 0.001
	var total float64
	for i := 0; i < 8; i++ {
		wid := "rotate-" + string(rune('a'+i))
		base, size, _, _, _, ok, _ := wm.claim(wid, wm.defaultBatch)
		if !ok {
			continue
		}
		_, _, p, _, _ := wm.submit(submitWorkRequest{
			WorkerID:    wid,
			BaseNonce:   base,
			BatchSize:   size,
			Attempts:    size,
			Found:       false,
			HashrateGHS: 50,
		})
		total += p
	}
	if total > 1.0 {
		t.Fatalf("rotation drained pool: total=%v", total)
	}
}
