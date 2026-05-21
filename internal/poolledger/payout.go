// Package poolledger defines pool payout accounting invariants (coordinator treasury vs workers).
package poolledger

import (
	"fmt"
	"math"
)

// DriftEpsilonHMC is the maximum allowed mismatch between Σ worker payouts and pool treasury.
const DriftEpsilonHMC = 0.000001

// LedgerDriftDetected is returned when treasury and worker sums diverge beyond DriftEpsilonHMC.
const LedgerDriftDetected = "Ledger Drift Detected"

// ComputeAttemptPayout returns HMC credited for a submit under payout_found_only rules.
// Mirrors cmd/coordinator/work.go submit payout accrual (attempt path, no hashrate cap).
func ComputeAttemptPayout(attempts uint64, rewardPerM float64, found bool, foundBonus float64, payoutFoundOnly bool) float64 {
	paidAttempts := attempts
	if payoutFoundOnly && !found {
		paidAttempts = 0
	}
	payout := (float64(paidAttempts) / 1_000_000.0) * rewardPerM
	if found {
		payout += foundBonus
	}
	if payout < 0 {
		return 0
	}
	return payout
}

// SumWorkerPayouts totals per-worker PayoutHMC fields.
func SumWorkerPayouts(workers map[string]float64) float64 {
	var sum float64
	for _, v := range workers {
		sum += v
	}
	return sum
}

// CheckTreasuryInvariant fails with Ledger Drift Detected when |Σ workers − treasury| ≥ ε.
func CheckTreasuryInvariant(workerSum, treasuryTotal float64) error {
	drift := math.Abs(workerSum - treasuryTotal)
	if drift >= DriftEpsilonHMC {
		return fmt.Errorf("%s: worker_sum=%.12f treasury=%.12f drift=%.12f epsilon=%.12f",
			LedgerDriftDetected, workerSum, treasuryTotal, drift, DriftEpsilonHMC)
	}
	return nil
}

// Accrue applies one payout to worker balances and treasury total; returns updated totals.
func Accrue(workers map[string]float64, workerID string, payout float64, treasury *float64) {
	if workers == nil {
		return
	}
	workers[workerID] += payout
	if treasury != nil {
		*treasury += payout
	}
}
