package main

import (
	"testing"

	"hackme/internal/workersettle"
)

// High (fimasw): decoy bare-base claim must not flip fleet export key and
// re-pay lifetime accrual under a new settler wid (SettledUnits=0).
func TestFleetFoldSettlementKeyChurnDoesNotDoublePay(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_AUTO_PRUNE_IDLE", "0")
	const addrA = "0x1111111111111111111111111111111111111111"
	const addrB = "0x2222222222222222222222222222222222222222"
	const units = uint64(1_000_000_000) // 10 HMC

	wm := &workManager{
		worker: map[string]workerPayoutStat{
			"rig-gpu0": {PayoutUnits: units, PayoutAddress: addrA},
		},
		abuse:           make(map[string]workerAbuseState),
		active:          make(map[workKey]leaseRecord),
		dropReasonCount: map[string]uint64{},
	}

	w1 := wm.stats(true)["workers"].(map[string]workerPayoutStat)
	if w1["rig"].PayoutUnits != units || w1["rig"].PayoutAddress != addrA {
		t.Fatalf("fold broken: %#v", w1["rig"])
	}
	if _, ok := w1["rig-gpu0"]; ok {
		t.Fatal("member should be folded into rig")
	}

	// Attacker: one claim on bare base id with a different address.
	wm.notePayoutLock("rig", addrB)

	w2 := wm.stats(true)["workers"].(map[string]workerPayoutStat)
	if _, ok := w2["rig-gpu0"]; ok {
		t.Fatal("member exported separately after decoy bare-base claim")
	}
	folded := w2["rig"]
	if folded.PayoutUnits != units {
		t.Fatalf("folded units=%d want %d", folded.PayoutUnits, units)
	}
	if folded.PayoutAddress != addrA {
		t.Fatalf("folded address=%q want miner %q (decoy must not win)", folded.PayoutAddress, addrA)
	}
	// Settler still sees the same export key; watermark stays continuous.
	d := workersettle.ComputeAccrualDelta(folded.PayoutUnits, units, 0)
	if d.DeltaUnits != 0 {
		t.Fatalf("expected zero unpaid after prior settle under 'rig', got delta=%d", d.DeltaUnits)
	}
}
