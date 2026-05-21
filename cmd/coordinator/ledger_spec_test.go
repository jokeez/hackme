package main

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"hackme/internal/poolledger"
)

// TestWorkManagerTreasuryLedger5000Integration drives real coordinator submit/claim paths
// with payout_found_only=0 and asserts no ledger drift vs poolledger invariant.
func TestWorkManagerTreasuryLedger5000Integration(t *testing.T) {
	wm := newTestWorkManagerForPayout(0.005, false)
	wm.hybridSignerEnabled = false
	rng := rand.New(rand.NewSource(99))
	var manualSum float64
	for i := 0; i < 5000; i++ {
		wid := "w-ledger-" + strconv.Itoa(i%200)
		batch := uint64(rng.Intn(500_000) + 1000)
		base, size, _, _, _, ok, reason := wm.claim(wid, uint64(i)*batch)
		if !ok {
			t.Fatalf("claim %d: %s", i, reason)
		}
		attempts := size
		if rng.Intn(5) == 0 {
			attempts = size / 2
		}
		_, reason, payout, _, _ := wm.submit(submitWorkRequest{
			WorkerID:    wid,
			BaseNonce:   base,
			BatchSize:   size,
			WorkID:      buildWorkID(wid, base, size),
			Attempts:    attempts,
			Found:       false,
			ResultHash:  "h-" + strconv.Itoa(i),
			HashrateGHS: 1.5 + rng.Float64()*10,
		})
		if reason != "" {
			t.Fatalf("submit %d reason=%q", i, reason)
		}
		manualSum += payout
	}
	st := wm.stats(true)
	total, _ := st["total_payout_hmc"].(float64)
	if err := poolledger.CheckTreasuryInvariant(manualSum, total); err != nil {
		t.Fatal(err)
	}
	workers, _ := st["workers"].(map[string]workerPayoutStat)
	var workerSum float64
	for _, w := range workers {
		workerSum += w.PayoutHMC
	}
	if err := poolledger.CheckTreasuryInvariant(workerSum, total); err != nil {
		t.Fatal(err)
	}
	if math.Abs(manualSum-workerSum) > 1e-9 {
		t.Fatalf("manual sum %f != worker map sum %f", manualSum, workerSum)
	}
}
