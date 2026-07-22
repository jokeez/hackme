package fuzzescrow

import "testing"

func TestComputeSplitUnits2080(t *testing.T) {
	const unitsPerHMC = 100_000_000
	total := uint64(10.0 * unitsPerHMC)
	s, err := ComputeSplitUnits(total, 100)
	if err != nil {
		t.Fatal(err)
	}
	if s.RunsPoolUnits < uint64(1.99*unitsPerHMC) || s.RunsPoolUnits > uint64(2.01*unitsPerHMC) {
		t.Fatalf("runs pool: %d", s.RunsPoolUnits)
	}
	if s.PerRunUnits == 0 {
		t.Fatal("per run zero")
	}
}

func TestBountyPayoutFee(t *testing.T) {
	miner, fee := BountyPayoutUnits(1_000_000_000)
	if fee == 0 || miner == 0 {
		t.Fatalf("miner=%d fee=%d", miner, fee)
	}
	if miner+fee != 1_000_000_000 {
		t.Fatalf("sum mismatch")
	}
}

func TestUniqueCrashBonusUnits(t *testing.T) {
	const unitsPerHMC = 100_000_000
	// 8 HMC bounty pool → 1% = 0.08 HMC but capped at 0.01 HMC.
	bonus := UniqueCrashBonusUnits(8 * unitsPerHMC)
	if bonus != UniqueCrashBonusMaxUnits {
		t.Fatalf("bonus=%d want max %d", bonus, UniqueCrashBonusMaxUnits)
	}
	if UniqueCrashBonusUnits(MinPerRunUnits/2) != 0 {
		t.Fatal("dust pool must yield zero bonus")
	}
	small := UniqueCrashBonusUnits(uint64(1.0 * unitsPerHMC)) // 1% of 1 HMC = 0.01 HMC
	if small != UniqueCrashBonusMaxUnits {
		t.Fatalf("1 HMC pool bonus=%d want %d", small, UniqueCrashBonusMaxUnits)
	}
	tiny := UniqueCrashBonusUnits(uint64(0.05 * unitsPerHMC)) // 1% = 0.0005 HMC = 50_000 units
	if tiny < MinPerRunUnits || tiny > UniqueCrashBonusMaxUnits {
		t.Fatalf("tiny bonus=%d out of band", tiny)
	}
}
