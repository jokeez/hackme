package fuzzescrow

import "testing"

// Truncation remainder must not strand in the runs pool: payable runs are an exact
// multiple of PerRunUnits, and Runs+Bounty == Total.
func TestComputeSplitUnitsSharePerRunConservation(t *testing.T) {
	cases := []struct {
		total, share float64
		budgetRuns   int
	}{
		{100, 0.2, 3}, // reporter seed shape (below MinCampaignRuns — use Min)
		{100, 0.2, 13},
		{100, 0.2, 99},
		{10, 0.2, 8},
		{40, 0.4, 2048},
		{7.5, 0.2, 64},
	}
	const unitsPerHMC = 100_000_000
	for _, c := range cases {
		runs := c.budgetRuns
		if runs < MinCampaignRuns {
			runs = MinCampaignRuns
		}
		total := uint64(c.total * unitsPerHMC)
		sp, err := ComputeSplitUnitsShare(total, runs, c.share)
		if err != nil {
			t.Fatalf("total=%v runs=%d share=%v: %v", c.total, runs, c.share, err)
		}
		if sp.RunsPoolUnits+sp.BountyPoolUnits != sp.TotalUnits {
			t.Fatalf("conservation: runs=%d bounty=%d total=%d", sp.RunsPoolUnits, sp.BountyPoolUnits, sp.TotalUnits)
		}
		if sp.PerRunUnits*uint64(runs) != sp.RunsPoolUnits {
			t.Fatalf("stranded runs dust: pool=%d per_run=%d runs=%d paid=%d",
				sp.RunsPoolUnits, sp.PerRunUnits, runs, sp.PerRunUnits*uint64(runs))
		}
	}
}

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

func TestComputeSplitUnits4060Deep(t *testing.T) {
	const unitsPerHMC = 100_000_000
	total := uint64(40.0 * unitsPerHMC)
	s, err := ComputeSplitUnitsShare(total, 2048, DeepRunsPoolShare)
	if err != nil {
		t.Fatal(err)
	}
	wantRuns := uint64(16.0 * unitsPerHMC) // 40% of 40 HMC
	if s.RunsPoolUnits != wantRuns {
		t.Fatalf("runs pool=%d want %d", s.RunsPoolUnits, wantRuns)
	}
	if s.PerRunUnits < MinPerRunUnits {
		t.Fatalf("per run too low: %d", s.PerRunUnits)
	}
	if EscrowSplitForDigPackage("deep") != EscrowSplit4060 {
		t.Fatal("deep split")
	}
	if EscrowSplitForDigPackage("audit") != EscrowSplit2080 {
		t.Fatal("audit split")
	}
	if MinBudgetHMCForDigPackage("deep") < MinDeepCampaignBudgetHMC {
		t.Fatal("deep min budget")
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
