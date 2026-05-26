package fuzzescrow

import "testing"

func TestComputeSplitRejectsDustPerRun(t *testing.T) {
	const unitsPerHMC = 100_000_000
	// 0.49 HMC fails min budget before per-run check in OpenFuzzEscrow; test per-run floor at 0.5/1000 runs.
	total := uint64(0.5 * unitsPerHMC)
	_, err := ComputeSplitUnits(total, 10_000)
	if err == nil {
		t.Fatal("expected per-run minimum error for huge run count")
	}
}

func TestComputeSplitAcceptsFairCampaign(t *testing.T) {
	const unitsPerHMC = 100_000_000
	total := uint64(1.0 * unitsPerHMC)
	s, err := ComputeSplitUnits(total, 64)
	if err != nil {
		t.Fatal(err)
	}
	if s.PerRunUnits < MinPerRunUnits {
		t.Fatalf("per run %d below min %d", s.PerRunUnits, MinPerRunUnits)
	}
}
