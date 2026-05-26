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
