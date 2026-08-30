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
