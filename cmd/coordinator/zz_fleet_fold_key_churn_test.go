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

// Incomplete-fix twin: PoH submit + fuzz adopt must share notePayoutLock's
// fleet-decoy guard (canAdoptBlankPayoutLocked), not only unpaid-blank.
func TestFleetFoldDecoyAdoptRefusedOnPoHAndFuzzPaths(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_AUTO_PRUNE_IDLE", "0")
	const addrA = "0x1111111111111111111111111111111111111111"
	const addrB = "0x2222222222222222222222222222222222222222"
	wm := &workManager{
		worker: map[string]workerPayoutStat{
			"rig-gpu0": {PayoutUnits: 1_000_000_000, PayoutAddress: addrA},
		},
		abuse:           make(map[string]workerAbuseState),
		active:          make(map[workKey]leaseRecord),
		dropReasonCount: map[string]uint64{},
	}
	wm.mu.Lock()
	st := wm.worker["rig"] // missing → zero
	if wm.canAdoptBlankPayoutLocked("rig", st) {
		t.Fatal("bare-base adopt must refuse while fleet member present")
	}
	if !wm.canAdoptBlankPayoutLocked("solo", st) {
		t.Fatal("non-fleet blank row should allow first adopt")
	}
	wm.mu.Unlock()

	// Memory must stay free of decoy after refused notePayoutLock.
	wm.notePayoutLock("rig", addrB)
	wm.mu.Lock()
	if _, ok := wm.worker["rig"]; ok {
		t.Fatalf("decoy bare-base row planted: %#v", wm.worker["rig"])
	}
	wm.mu.Unlock()
}

// High: even if a 0-accrual decoy is already in memory (race / pre-fix path),
// fold must not remount unpaid -gpuN under a new settler wid.
func TestFleetFoldLiveDecoyDoesNotRemountMemberKey(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_AUTO_PRUNE_IDLE", "0")
	const addrA = "0x1111111111111111111111111111111111111111"
	const addrB = "0x2222222222222222222222222222222222222222"
	const units = uint64(1_000_000_000)
	wm := &workManager{
		worker: map[string]workerPayoutStat{
			"rig-gpu0": {PayoutUnits: units, PayoutAddress: addrA},
			// Force-planted decoy (bypasses notePayoutLock) — defense in depth.
			"rig": {PayoutAddress: addrB},
		},
		abuse:           make(map[string]workerAbuseState),
		active:          make(map[workKey]leaseRecord),
		dropReasonCount: map[string]uint64{},
	}
	w := wm.stats(true)["workers"].(map[string]workerPayoutStat)
	if _, ok := w["rig-gpu0"]; ok {
		t.Fatalf("live decoy remounted member: %#v", w)
	}
	folded := w["rig"]
	if folded.PayoutUnits != units || folded.PayoutAddress != addrA {
		t.Fatalf("fold after live decoy: %#v", folded)
	}
	d := workersettle.ComputeAccrualDelta(folded.PayoutUnits, units, 0)
	if d.DeltaUnits != 0 {
		t.Fatalf("watermark reopen delta=%d", d.DeltaUnits)
	}
}

// Claim-before-members: decoy plants while no -gpuN in memory; later member
// must still fold under 'rig' (folded unpaid gate), not remount.
func TestFleetFoldClaimBeforeMembersDoesNotRemount(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_AUTO_PRUNE_IDLE", "0")
	const addrA = "0x1111111111111111111111111111111111111111"
	const addrB = "0x2222222222222222222222222222222222222222"
	const units = uint64(1_000_000_000)
	wm := &workManager{
		worker:          map[string]workerPayoutStat{},
		abuse:           make(map[string]workerAbuseState),
		active:          make(map[workKey]leaseRecord),
		dropReasonCount: map[string]uint64{},
		maxWorkers:      100,
	}
	wm.notePayoutLock("rig", addrB) // members absent → plant allowed
	wm.mu.Lock()
	if wm.worker["rig"].PayoutAddress != addrB {
		t.Fatalf("expected decoy plant when members absent: %#v", wm.worker["rig"])
	}
	wm.worker["rig-gpu0"] = workerPayoutStat{PayoutUnits: units, PayoutAddress: addrA}
	wm.mu.Unlock()
	w := wm.stats(true)["workers"].(map[string]workerPayoutStat)
	if _, ok := w["rig-gpu0"]; ok {
		t.Fatalf("claim-before-members remounted: %#v", w)
	}
	folded := w["rig"]
	if folded.PayoutUnits != units || folded.PayoutAddress != addrA {
		t.Fatalf("fold after claim-before-members: %#v", folded)
	}
	d := workersettle.ComputeAccrualDelta(folded.PayoutUnits, units, 0)
	if d.DeltaUnits != 0 {
		t.Fatalf("watermark reopen delta=%d", d.DeltaUnits)
	}
}

// Real unpaid address conflict must still eject the stranger member.
func TestFleetFoldUnpaidConflictStillExportsSeparately(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_AUTO_PRUNE_IDLE", "0")
	const addrA = "0x1111111111111111111111111111111111111111"
	const addrB = "0x2222222222222222222222222222222222222222"
	wm := &workManager{
		worker: map[string]workerPayoutStat{
			"rig":      {PayoutUnits: 100_000_000, PayoutAddress: addrA},
			"rig-gpu0": {PayoutUnits: 200_000_000, PayoutAddress: addrB},
		},
		abuse:           make(map[string]workerAbuseState),
		active:          make(map[workKey]leaseRecord),
		dropReasonCount: map[string]uint64{},
	}
	w := wm.stats(true)["workers"].(map[string]workerPayoutStat)
	if _, ok := w["rig-gpu0"]; !ok {
		t.Fatalf("unpaid conflict must export member separately: %#v", w)
	}
	if w["rig"].PayoutAddress != addrA || w["rig"].PayoutUnits != 100_000_000 {
		t.Fatalf("base row: %#v", w["rig"])
	}
}
