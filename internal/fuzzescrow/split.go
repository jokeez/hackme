// Package fuzzescrow defines the hybrid 20/80 fuzz campaign budget split (ratios only).
package fuzzescrow

import "errors"

const (
	RunsPoolShare         = 0.20
	BountyPoolShare       = 0.80
	BountyPlatformFeeRate = 0.05 // taken from bounty payout only
	// UniqueCrashBonusRate is a small share of the bounty pool paid once for a unique crash.
	// Main medium/high/critical bounty stays confirmed-native and receives the remainder.
	UniqueCrashBonusRate = 0.01
	// UniqueCrashBonusMaxUnits caps the crash bonus at 0.01 HMC (1e8 units/HMC).
	UniqueCrashBonusMaxUnits = 1_000_000
	// MinCampaignBudgetHMC prevents dust campaigns that pay miners negligible per-run slices.
	MinCampaignBudgetHMC = 0.5
	MaxCampaignBudgetHMC = 500.0
	MinCampaignRuns      = 8
	MaxCampaignRuns      = 10_000_000
	// MinPerRunUnits is the minimum HMC paid per verified fuzz run (0.0001 HMC at 1e8 units/HMC).
	MinPerRunUnits = 10_000
)

// SplitUnits is the unit breakdown for a fuzz campaign escrow lock.
type SplitUnits struct {
	TotalUnits      uint64
	RunsPoolUnits   uint64
	BountyPoolUnits uint64
	PerRunUnits     uint64
}

// ComputeSplitUnits validates inputs and returns the 20/80 split with per-run rate.
func ComputeSplitUnits(totalUnits uint64, budgetRuns int) (SplitUnits, error) {
	if totalUnits == 0 {
		return SplitUnits{}, errors.New("fuzz escrow: budget rounds to zero units")
	}
	if budgetRuns < MinCampaignRuns {
		return SplitUnits{}, errors.New("fuzz escrow: budget_runs below minimum")
	}
	if budgetRuns > MaxCampaignRuns {
		return SplitUnits{}, errors.New("fuzz escrow: budget_runs too large")
	}
	runsUnits := uint64(float64(totalUnits) * RunsPoolShare)
	bountyUnits := totalUnits - runsUnits
	perRun := runsUnits / uint64(budgetRuns)
	if perRun == 0 {
		return SplitUnits{}, errors.New("fuzz escrow: per-run payout rounds to zero; increase budget or lower runs")
	}
	if perRun < MinPerRunUnits {
		return SplitUnits{}, errors.New("fuzz escrow: per-run payout below minimum; increase budget_hmc or reduce budget_runs")
	}
	return SplitUnits{
		TotalUnits:      totalUnits,
		RunsPoolUnits:   runsUnits,
		BountyPoolUnits: bountyUnits,
		PerRunUnits:     perRun,
	}, nil
}

// BountyPayoutUnits returns miner and platform fee units for a bounty release.
func BountyPayoutUnits(bountyPoolUnits uint64) (minerUnits, feeUnits uint64) {
	feeUnits = uint64(float64(bountyPoolUnits) * BountyPlatformFeeRate)
	if feeUnits > bountyPoolUnits {
		feeUnits = bountyPoolUnits
	}
	return bountyPoolUnits - feeUnits, feeUnits
}

// UniqueCrashBonusUnits returns a one-shot unique-crash bonus from the bounty pool.
// Never exceeds UniqueCrashBonusMaxUnits / 1% of pool / 10% of pool; returns 0 when dust.
func UniqueCrashBonusUnits(bountyPoolUnits uint64) uint64 {
	if bountyPoolUnits == 0 {
		return 0
	}
	bonus := uint64(float64(bountyPoolUnits) * UniqueCrashBonusRate)
	if bonus > UniqueCrashBonusMaxUnits {
		bonus = UniqueCrashBonusMaxUnits
	}
	if maxShare := bountyPoolUnits / 10; maxShare > 0 && bonus > maxShare {
		bonus = maxShare
	}
	if bonus < MinPerRunUnits {
		return 0
	}
	if bonus > bountyPoolUnits {
		return bountyPoolUnits
	}
	return bonus
}
