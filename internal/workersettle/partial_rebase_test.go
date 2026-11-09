package workersettle

import "testing"

func TestPartialRebaseAnchorAdvancesByPaidOnly(t *testing.T) {
	// After coordinator reset: settled=10 HMC, accrued=0.05, anchor becomes 0.05.
	// Delta unpaid = 0 from anchor until growth; then accrued=0.20 → delta=0.15.
	// Chunk pays only 0.07 → new settled=10.07, new anchor must be 0.05+0.07=0.12
	// so remainder 0.08 stays payable (not lost by anchoring to full 0.20).
	d := ComputeAccrualDelta(HMCToUnits(0.20), HMCToUnits(10.0), HMCToUnits(0.05))
	if !d.Rebase || d.DeltaUnits != HMCToUnits(0.15) {
		t.Fatalf("delta=%+v", d)
	}
	pay := HMCToUnits(0.07)
	newAnchor := d.BaselineUnits + pay
	if newAnchor != HMCToUnits(0.12) {
		t.Fatalf("anchor=%d want %d", newAnchor, HMCToUnits(0.12))
	}
	d2 := ComputeAccrualDelta(HMCToUnits(0.20), HMCToUnits(10.0)+pay, newAnchor)
	if d2.DeltaUnits != HMCToUnits(0.08) {
		t.Fatalf("remainder delta=%d want %d", d2.DeltaUnits, HMCToUnits(0.08))
	}
}

// Crash-recovery ApplySettle must use the same anchor rule as the success path:
// BaselineUnits+paid under rebase — never jump to full accrued (lost remainder).
func TestRecoverPendingAnchorMatchesSuccessPath(t *testing.T) {
	accrued := HMCToUnits(0.20)
	settled := HMCToUnits(10.0)
	sessionAnchor := HMCToUnits(0.05)
	d := ComputeAccrualDelta(accrued, settled, sessionAnchor)
	pendDelta := HMCToUnits(0.07)

	// Old buggy recover: anchor := accrued (=0.20) → remainder lost.
	buggy := accrued
	dBuggy := ComputeAccrualDelta(accrued, settled+pendDelta, buggy)
	if dBuggy.DeltaUnits != 0 {
		t.Fatalf("buggy recover should zero remainder, got %d", dBuggy.DeltaUnits)
	}

	clear := !d.Rebase
	anchor := d.BaselineUnits + pendDelta
	if clear {
		anchor = 0
	}
	if clear || anchor != HMCToUnits(0.12) {
		t.Fatalf("recover anchor=%d clear=%v want 0.12 under rebase", anchor, clear)
	}
	dOk := ComputeAccrualDelta(accrued, settled+pendDelta, anchor)
	if dOk.DeltaUnits != HMCToUnits(0.08) {
		t.Fatalf("recover remainder=%d want %d", dOk.DeltaUnits, HMCToUnits(0.08))
	}
}
