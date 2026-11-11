package main

import (
	"testing"

	"hackme/internal/workersettle"
)

// Follow-up High (fimasw): claim bare base FIRST, then mine as -gpuN, prune decoy,
// fold back under base — must not double-pay under a new settle wid.
func TestFleetFoldReverseOrderDoesNotDoublePay(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_AUTO_PRUNE_IDLE", "0")
	const addrA = "0x1111111111111111111111111111111111111111"
	const addrB = "0x2222222222222222222222222222222222222222"
	const units = uint64(1_000_000_000)

	wm := &workManager{
		worker: map[string]workerPayoutStat{
			"rig":      {PayoutUnits: 0, PayoutAddress: addrB},
			"rig-gpu0": {PayoutUnits: units, PayoutAddress: addrA},
		},
		abuse:           make(map[string]workerAbuseState),
		active:          make(map[workKey]leaseRecord),
		dropReasonCount: map[string]uint64{},
	}

	out1 := wm.stats(true)["workers"].(map[string]workerPayoutStat)
	if _, ok := out1["rig-gpu0"]; ok {
		// If still exported separately, settle under member wid
		d1 := workersettle.ComputeAccrualDelta(out1["rig-gpu0"].PayoutUnits, 0, 0)
		if d1.DeltaUnits != units {
			t.Fatalf("unexpected delta1=%d", d1.DeltaUnits)
		}
		delete(wm.worker, "rig")
		out2 := wm.stats(true)["workers"].(map[string]workerPayoutStat)
		folded := out2["rig"]
		d2 := workersettle.ComputeAccrualDelta(folded.PayoutUnits, 0, 0)
		if d2.DeltaUnits == units {
			t.Fatalf("DOUBLE PAY: %d under rig-gpu0 then %d under rig", d1.DeltaUnits, d2.DeltaUnits)
		}
		t.Fatalf("member still exported separately after reverse-order decoy (fold unpaid check should keep fold)")
	}
	// Preferred: stays folded; decoy addr dropped; one settle key
	folded := out1["rig"]
	if folded.PayoutUnits != units {
		t.Fatalf("folded units=%d", folded.PayoutUnits)
	}
	if folded.PayoutAddress != addrA && !folded.AddressConflict {
		// decoy drop should leave miner addr
		t.Fatalf("addr=%q conflict=%v", folded.PayoutAddress, folded.AddressConflict)
	}
	d := workersettle.ComputeAccrualDelta(folded.PayoutUnits, units, 0)
	if d.DeltaUnits != 0 {
		t.Fatalf("want continuous watermark delta0 got %d", d.DeltaUnits)
	}
}
