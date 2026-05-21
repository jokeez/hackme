package chain

import (
	mathrand "math/rand"
	"strconv"
	"testing"

	"hackme/internal/poolledger"
)

// TestPoolTreasuryLedgerConsistency5000 simulates 5000 random pool payouts (payout_found_only=0)
// and asserts Σ worker balances equals pool treasury total within DriftEpsilonHMC.
func TestPoolTreasuryLedgerConsistency5000(t *testing.T) {
	rng := mathrand.New(mathrand.NewSource(42))
	workers := make(map[string]float64)
	var treasury float64
	rewardValues := []float64{
		0.001, 0.005, 0.0050000001, 0.123456789, 1.0, 0.0000001,
		3.3333333, 0.25, 0.000001,
	}
	for i := 0; i < 5000; i++ {
		wid := "w-" + strconv.Itoa(i%137)
		attempts := uint64(rng.Intn(4_000_000) + 1)
		rewardPerM := rewardValues[rng.Intn(len(rewardValues))]
		found := rng.Intn(20) == 0
		foundBonus := 0.01
		if found && rng.Intn(2) == 0 {
			foundBonus = 0.25
		}
		payout := poolledger.ComputeAttemptPayout(attempts, rewardPerM, found, foundBonus, false)
		poolledger.Accrue(workers, wid, payout, &treasury)
	}
	workerSum := poolledger.SumWorkerPayouts(workers)
	if err := poolledger.CheckTreasuryInvariant(workerSum, treasury); err != nil {
		t.Fatal(err)
	}
	if treasury <= 0 {
		t.Fatal("expected positive treasury accrual over 5000 payouts")
	}
}

// TestPoolTreasuryLedgerFoundOnlySkipsAttempts ensures payout_found_only=1 never drifts treasury.
func TestPoolTreasuryLedgerFoundOnlySkipsAttempts(t *testing.T) {
	workers := make(map[string]float64)
	var treasury float64
	for i := 0; i < 500; i++ {
		payout := poolledger.ComputeAttemptPayout(1_000_000, 1.0, false, 0.25, true)
		if payout != 0 {
			t.Fatalf("non-found submit must not pay under found-only, got %v", payout)
		}
		poolledger.Accrue(workers, "w", payout, &treasury)
	}
	if err := poolledger.CheckTreasuryInvariant(poolledger.SumWorkerPayouts(workers), treasury); err != nil {
		t.Fatal(err)
	}
}

