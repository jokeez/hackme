package hms

import (
	"testing"
)

func TestSealEpochBudgetUnits(t *testing.T) {
	base := SealEpochBudgetUnits(0)
	if base != 1_000_000 {
		t.Fatalf("base budget = %d want 1_000_000", base)
	}
	withPrepaid := SealEpochBudgetUnits(10.0)
	wantBonus := hmsToUnits(10.0 * SealBudgetPrepaidShareRate)
	if withPrepaid != base+wantBonus {
		t.Fatalf("prepaid budget = %d want %d", withPrepaid, base+wantBonus)
	}
}

func TestComputeSealEpochPayoutsMultiWorker(t *testing.T) {
	budget := uint64(1_000_000)
	shares := map[string]uint64{
		"winner": 100,
		"w2":     300,
		"w3":     100,
	}
	lines, err := ComputeSealEpochPayouts(budget, "winner", shares)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]uint64{}
	var sum uint64
	for _, line := range lines {
		got[line.WorkerID] = line.TotalUnits
		sum += line.TotalUnits
	}
	if sum != budget {
		t.Fatalf("sum=%d budget=%d", sum, budget)
	}
	if got["winner"] != 800_000 {
		t.Fatalf("winner=%d want 800000 (750k + 50k part)", got["winner"])
	}
	if got["w2"] != 150_000 {
		t.Fatalf("w2=%d want 150000", got["w2"])
	}
	if got["w3"] != 50_000 {
		t.Fatalf("w3=%d want 50000", got["w3"])
	}
}

func TestComputeSealEpochPayoutsWinnerOnly(t *testing.T) {
	budget := uint64(2_000_000)
	lines, err := ComputeSealEpochPayouts(budget, "solo", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0].TotalUnits != budget {
		t.Fatalf("lines=%+v", lines)
	}
}

func TestComputeSealEpochPayoutsWinnerWithoutShares(t *testing.T) {
	budget := uint64(1_000_000)
	shares := map[string]uint64{"other": 400}
	lines, err := ComputeSealEpochPayouts(budget, "winner", shares)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]uint64{}
	for _, line := range lines {
		got[line.WorkerID] = line.TotalUnits
	}
	if got["winner"] != 750_000 {
		t.Fatalf("winner=%d want 750000", got["winner"])
	}
	if got["other"] != 250_000 {
		t.Fatalf("other=%d want 250000", got["other"])
	}
}

func TestComputeSealEpochPayoutsRemainderDeterministic(t *testing.T) {
	budget := uint64(10)
	shares := map[string]uint64{"a": 1, "b": 1, "c": 1}
	lines, err := ComputeSealEpochPayouts(budget, "a", shares)
	if err != nil {
		t.Fatal(err)
	}
	var sum uint64
	for _, line := range lines {
		sum += line.TotalUnits
	}
	if sum != budget {
		t.Fatalf("sum=%d budget=%d lines=%+v", sum, budget, lines)
	}
}

func TestSealRewardPolicyHashStable(t *testing.T) {
	h1 := SealRewardPolicyHash()
	h2 := SealRewardPolicyHash()
	if h1 == "" || h1 != h2 {
		t.Fatalf("policy hash unstable: %q %q", h1, h2)
	}
}
