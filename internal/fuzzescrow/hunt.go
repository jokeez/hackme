package fuzzescrow

import (
	"errors"
	"strings"
)

const (
	HuntRunsPoolShare   = 0.50
	HuntBountyPoolShare = 0.50
	// HuntUniqueCrashBonusMaxUnits caps Hunt crash bonus at 0.05 HMC.
	HuntUniqueCrashBonusMaxUnits = 5_000_000
	// HuntMinPerShardUnits is minimum HMC per verified Hunt shard (0.002 HMC).
	HuntMinPerShardUnits     = 200_000
	HuntMinLiteBudgetHMC     = 15.0
	HuntMinStandardBudgetHMC = 50.0
	HuntMinHeavyBudgetHMC    = 150.0
	HuntMinShards            = 8
	EscrowSplit2080          = "20_80"
	EscrowSplit5050          = "50_50"
)

// ComputeHuntSplitUnits validates Hunt inputs and returns the 50/50 split.
func ComputeHuntSplitUnits(totalUnits uint64, budgetShards int) (SplitUnits, error) {
	if totalUnits == 0 {
		return SplitUnits{}, errors.New("hunt escrow: budget rounds to zero units")
	}
	if budgetShards < HuntMinShards {
		return SplitUnits{}, errors.New("hunt escrow: budget_shards below minimum")
	}
	if budgetShards > MaxCampaignRuns {
		return SplitUnits{}, errors.New("hunt escrow: budget_shards too large")
	}
	runsUnits := uint64(float64(totalUnits) * HuntRunsPoolShare)
	bountyUnits := totalUnits - runsUnits
	perShard := runsUnits / uint64(budgetShards)
	if perShard == 0 {
		return SplitUnits{}, errors.New("hunt escrow: per-shard payout rounds to zero; increase budget or lower shards")
	}
	if perShard < HuntMinPerShardUnits {
		return SplitUnits{}, errors.New("hunt escrow: per-shard payout below minimum; increase budget_hmc or reduce budget_shards")
	}
	return SplitUnits{
		TotalUnits:      totalUnits,
		RunsPoolUnits:   runsUnits,
		BountyPoolUnits: bountyUnits,
		PerRunUnits:     perShard,
	}, nil
}

// UniqueCrashBonusUnitsForSplit returns crash bonus using Dig or Hunt cap.
func UniqueCrashBonusUnitsForSplit(bountyPoolUnits uint64, escrowSplit string) uint64 {
	switch escrowSplit {
	case EscrowSplit5050:
		return uniqueCrashBonusUnitsCapped(bountyPoolUnits, HuntUniqueCrashBonusMaxUnits)
	default:
		return UniqueCrashBonusUnits(bountyPoolUnits)
	}
}

func uniqueCrashBonusUnitsCapped(bountyPoolUnits, maxUnits uint64) uint64 {
	if bountyPoolUnits == 0 || maxUnits == 0 {
		return 0
	}
	bonus := uint64(float64(bountyPoolUnits) * UniqueCrashBonusRate)
	if bonus > maxUnits {
		bonus = maxUnits
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

// MinBudgetHMCForHuntPackage returns floor budget for lite|standard|heavy packages.
func MinBudgetHMCForHuntPackage(pkg string) float64 {
	switch pkg {
	case "hunt_heavy", "heavy":
		return HuntMinHeavyBudgetHMC
	case "hunt_standard", "standard":
		return HuntMinStandardBudgetHMC
	case "hunt_lite", "lite", "":
		return HuntMinLiteBudgetHMC
	default:
		return HuntMinLiteBudgetHMC
	}
}

// HuntBountyShare returns the fraction of the remaining bounty pool paid for severity (Hunt 50/50 only).
func HuntBountyShare(severity string) float64 {
	switch strings.TrimSpace(strings.ToLower(severity)) {
	case "critical":
		return 1.0
	case "high":
		return 0.6
	default:
		return 0
	}
}

// HuntBountyPayoutUnits pays a severity-weighted slice of the remaining bounty pool (before platform fee).
func HuntBountyPayoutUnits(remaining uint64, severity string) (minerUnits, feeUnits uint64, ok bool) {
	share := HuntBountyShare(severity)
	if share <= 0 || remaining == 0 {
		return 0, 0, false
	}
	slice := uint64(float64(remaining) * share)
	if slice < MinPerRunUnits {
		return 0, 0, false
	}
	minerUnits, feeUnits = BountyPayoutUnits(slice)
	return minerUnits, feeUnits, true
}
