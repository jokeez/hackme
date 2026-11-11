package main

import (
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/store"
)

// Med: fuzz submit adopt must prefetch durable unpaid (H2 twin of notePayoutLock)
// and must not plant a bare-base row beside -gpuN members. A clean first adopt
// pins worker_payout_lock so a restart cannot free the settle key.
func TestFuzzAdoptPrefetchUnpaidAndPinsFreshLock(t *testing.T) {
	t.Setenv("HACKME_COORDINATOR_AUTO_PRUNE_IDLE", "0")
	const addrA = "0x1111111111111111111111111111111111111111"
	const addrB = "0x2222222222222222222222222222222222222222"

	wm := &workManager{
		worker: map[string]workerPayoutStat{
			"rig-gpu0": {PayoutUnits: 1_000_000_000, PayoutAddress: addrA},
		},
		abuse:           map[string]workerAbuseState{},
		active:          map[workKey]leaseRecord{},
		dropReasonCount: map[string]uint64{},
	}
	wm.adoptFuzzPayoutLock("rig", addrB)
	wm.mu.Lock()
	if row, ok := wm.worker["rig"]; ok {
		t.Fatalf("fuzz adopt planted bare-base row: %#v", row)
	}
	wm.mu.Unlock()

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "fuzz-adopt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	wm2 := &workManager{worker: map[string]workerPayoutStat{}}
	wm2.attachDedupDB(db)
	wm2.mu.Lock()
	wm2.worker["sniped"] = workerPayoutStat{PayoutHMC: 3, AcceptedAtt: 10}
	wm2.mu.Unlock()
	wm2.persistWorkerAccrual("sniped", true)
	wm2.mu.Lock()
	delete(wm2.worker, "sniped")
	wm2.mu.Unlock()

	wm2.adoptFuzzPayoutLock("sniped", addrB)
	wm2.mu.Lock()
	got := strings.TrimSpace(wm2.worker["sniped"].PayoutAddress)
	wm2.mu.Unlock()
	if got != "" {
		t.Fatalf("unpaid-blank fuzz adopt set address %q", got)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(1) FROM worker_payout_lock WHERE worker_id=?`, "sniped").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("unpaid-blank adopt must not pin a settle lock")
	}

	wm2.adoptFuzzPayoutLock("fresh", addrA)
	wm2.mu.Lock()
	fresh := strings.TrimSpace(wm2.worker["fresh"].PayoutAddress)
	wm2.mu.Unlock()
	if !strings.EqualFold(fresh, addrA) {
		t.Fatalf("fresh adopt address=%q", fresh)
	}
	var locked string
	if err := db.QueryRow(`SELECT payout_address FROM worker_payout_lock WHERE worker_id=?`, "fresh").Scan(&locked); err != nil {
		t.Fatal(err)
	}
	if !strings.EqualFold(locked, addrA) {
		t.Fatalf("durable settle pin=%q want %s", locked, addrA)
	}
}
