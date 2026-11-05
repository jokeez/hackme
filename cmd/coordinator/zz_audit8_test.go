package main

import (
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
	"hackme/internal/workersettle"
)

// A1: nextNonce is not persisted. After a coordinator restart on the same tip, ranges are
// re-issued from base 0 and shares already paid before the restart validate again
// (Blake3(prev||addr||nonce) has no lease/session binding; no durable share dedup).
func TestAudit8RestartReplaysPaidShares(t *testing.T) {
	const addr = "HMC-aaaaaaaaaaaaaaaa"
	wm1 := secWM(t)
	l1 := secClaim(t, wm1, "w-replay", addr)
	shares := honestShares(l1, l1.prev, addr, 64)
	if len(shares) == 0 {
		t.Skip("no shares")
	}
	reason, pay1 := secSubmit(wm1, "w-replay", l1, shares)
	if reason != "" || pay1 <= 0 {
		t.Fatalf("first submit: %q %v", reason, pay1)
	}

	// "Restart": fresh manager, same tip (chain stalled / restart within the block).
	wm2 := secWM(t)
	l2 := secClaim(t, wm2, "w-replay", addr)
	if l2.base == l1.base {
		t.Fatalf("restart re-issued the same range %d+%d", l2.base, l2.size)
	}
	// Even if a miner forces the old nonces into the new range, they are outside it.
	// No hashing: replay the nonces recorded before the restart.
	reason, pay2 := secSubmit(wm2, "w-replay", l2, shares)
	if reason == "" && pay2 > 0 {
		t.Errorf("BUG: pre-restart shares paid again after restart: first=%v replay=%v", pay1, pay2)
	}
}

// A2: fleet -gpuN rows are merged into one settle key. Idle prune drops a member from memory
// (merged accrual falls), the settler anchors; rehydration of the member's durable lifetime
// accrual is then read as new growth above the anchor and paid again.
func TestAudit8FleetPruneRehydrateDoublePays(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "fleet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	t.Setenv("HACKME_COORDINATOR_POOL_DAILY_BUDGET_HMC", "0")
	wm := &workManager{
		worker:     map[string]workerPayoutStat{},
		abuse:      map[string]workerAbuseState{},
		ipAbuse:    map[string]workerAbuseState{},
		active:     map[workKey]leaseRecord{},
		maxWorkers: 100,
	}
	wm.attachDedupDB(db)
	const addr = "HMC-bbbbbbbbbbbbbbbb"
	old := time.Now().Unix() - 3600
	for _, id := range []string{"rig-gpu0", "rig-gpu1", "rig-gpu2"} {
		wm.worker[id] = workerPayoutStat{PayoutUnits: 100_000_000, PayoutHMC: 1, PayoutAddress: addr, LastSeenUnix: old}
		wm.persistWorkerAccrual(id, true)
	}
	accrued := func() uint64 {
		wm.mu.Lock()
		defer wm.mu.Unlock()
		var sum uint64
		for id, st := range wm.worker {
			if fleetBaseWorkerID(id) == "rig" {
				sum += st.PayoutUnits
			}
		}
		return sum
	}
	// Settler ledger for key "rig".
	var settled, anchor, paid uint64
	settle := func() {
		d := workersettle.ComputeAccrualDelta(accrued(), settled, anchor)
		if d.NeedAnchor {
			anchor = d.NewAnchorUnits
		} else if d.ClearAnchor {
			anchor = 0
		}
		if d.DeltaUnits > 0 {
			paid += d.DeltaUnits
			settled += d.DeltaUnits
			if d.Rebase {
				anchor = d.BaselineUnits + d.DeltaUnits
			} else {
				anchor = 0
			}
		}
	}
	settle() // pays 3 HMC lifetime
	if paid != 300_000_000 {
		t.Fatalf("initial paid=%d", paid)
	}
	// gpu1 + gpu2 idle 10 min, lifetime <= 2 HMC → auto pruned from memory (durable kept).
	wm.mu.Lock()
	wm.pruneStaleWorkersLocked("rig-gpu1", 2.0, 600, false, false, time.Now().Unix())
	wm.pruneStaleWorkersLocked("rig-gpu2", 2.0, 600, false, false, time.Now().Unix())
	wm.mu.Unlock()
	settle() // anchors at 1 HMC
	// gpu1 comes back: next submit prefetches durable 1 HMC.
	wm.prefetchWorkerAccrual("rig-gpu1")
	settle()
	wm.prefetchWorkerAccrual("rig-gpu2")
	settle()
	// Now in rebase mode: every single-member offline/online cycle re-pays its lifetime.
	for i := 0; i < 5; i++ {
		wm.mu.Lock()
		st := wm.worker["rig-gpu1"]
		st.LastSeenUnix = old
		wm.worker["rig-gpu1"] = st
		wm.pruneStaleWorkersLocked("rig-gpu1", 2.0, 600, false, false, time.Now().Unix())
		wm.mu.Unlock()
		settle()
		wm.prefetchWorkerAccrual("rig-gpu1")
		settle()
	}
	if paid > 300_000_000 {
		t.Errorf("BUG: fleet paid %d units for 300000000 units of work (double pay %d)", paid, paid-300_000_000)
	}
}
