package fuzzescrow

import "testing"

func TestComputeHuntSplitUnits5050(t *testing.T) {
	const unitsPerHMC = 100_000_000
	total := uint64(20.0 * unitsPerHMC)
	s, err := ComputeHuntSplitUnits(total, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if s.RunsPoolUnits != total/2 || s.BountyPoolUnits != total/2 {
		t.Fatalf("split not 50/50: runs=%d bounty=%d", s.RunsPoolUnits, s.BountyPoolUnits)
	}
	if s.PerRunUnits < HuntMinPerShardUnits {
		t.Fatalf("per shard %d below min %d", s.PerRunUnits, HuntMinPerShardUnits)
	}
}

func TestHuntCrashBonusCap(t *testing.T) {
	const unitsPerHMC = 100_000_000
	bonus := UniqueCrashBonusUnitsForSplit(30*unitsPerHMC, EscrowSplit5050)
	if bonus != HuntUniqueCrashBonusMaxUnits {
		t.Fatalf("bonus=%d want %d", bonus, HuntUniqueCrashBonusMaxUnits)
	}
	dig := UniqueCrashBonusUnitsForSplit(8*unitsPerHMC, EscrowSplit2080)
	if dig != UniqueCrashBonusMaxUnits {
		t.Fatalf("dig bonus=%d want %d", dig, UniqueCrashBonusMaxUnits)
	}
}

func TestHuntBountyPayoutUnitsCriticalFullSlice(t *testing.T) {
	const unitsPerHMC = 100_000_000
	remaining := uint64(10 * unitsPerHMC)
	miner, fee, ok := HuntBountyPayoutUnits(remaining, "critical")
	if !ok {
		t.Fatal("critical should be payable")
	}
	if miner+fee != remaining {
		t.Fatalf("critical should consume full remaining: miner=%d fee=%d remaining=%d", miner, fee, remaining)
	}
}

func TestHuntBountyPayoutUnitsHighPays60Percent(t *testing.T) {
	const unitsPerHMC = 100_000_000
	remaining := uint64(10 * unitsPerHMC)
	miner, fee, ok := HuntBountyPayoutUnits(remaining, "high")
	if !ok {
		t.Fatal("high should be payable")
	}
	slice := miner + fee
	want := uint64(float64(remaining) * 0.6)
	if slice < want-2 || slice > want+2 {
		t.Fatalf("high slice=%d want ~%d", slice, want)
	}
}

func TestHuntBountyPayoutUnitsMediumNotPayable(t *testing.T) {
	miner, fee, ok := HuntBountyPayoutUnits(10_000_000, "medium")
	if ok || miner != 0 || fee != 0 {
		t.Fatalf("medium should not pay: ok=%v miner=%d fee=%d", ok, miner, fee)
	}
}

func TestMinBudgetHMCForHuntHeavy(t *testing.T) {
	if got := MinBudgetHMCForHuntPackage("hunt_heavy"); got != HuntMinHeavyBudgetHMC {
		t.Fatalf("heavy min=%v want %v", got, HuntMinHeavyBudgetHMC)
	}
}
