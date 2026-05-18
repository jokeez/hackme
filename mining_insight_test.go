package main

import (
	"math"
	"testing"
)

func TestComputeMiningInsight(t *testing.T) {
	eta, prog, proj := computeMiningInsight(0, 1000, 500, 0.01)
	if math.Abs(eta-2.0) > 0.01 {
		t.Fatalf("eta want ~2s got %v", eta)
	}
	if prog != 0 {
		t.Fatalf("prog at 0 attempts want 0 got %v", prog)
	}
	if math.Abs(proj-18.0) > 0.05 { // 0.01 * 3600/2
		t.Fatalf("projected HMC/h want ~18 got %v", proj)
	}

	eta2, _, _ := computeMiningInsight(100, 1000, 10, 0.01)
	if math.Abs(eta2-100.0) > 0.01 {
		t.Fatalf("eta2 got %v", eta2)
	}

	eta3, _, _ := computeMiningInsight(0, 250, 100, 0.01) // below min mod → clamped to 251
	if math.Abs(eta3-2.51) > 0.01 {
		t.Fatalf("eta3 clamp mod got %v", eta3)
	}

	eta4, _, _ := computeMiningInsight(0, 1000, 0, 0.01)
	if eta4 >= 0 {
		t.Fatalf("zero rate want -1 got %v", eta4)
	}
}
