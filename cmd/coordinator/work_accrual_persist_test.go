package main

import (
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestRevokeWorkerDeletesDurableAccrual(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual-revoke.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{
		worker:         map[string]workerPayoutStat{},
		abuse:          map[string]workerAbuseState{},
		ipAbuse:        map[string]workerAbuseState{},
		totalPayoutHMC: 5,
		totalPayoutSUP: 0.2,
	}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["evil"] = workerPayoutStat{
		AcceptedAtt:   9_000_000,
		PayoutHMC:     5,
		PayoutSUP:     0.2,
		PayoutAddress: "HMC-evil",
		LastClientIP:  "9.9.9.9",
	}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("evil", true)
	if _, ok := wm.fetchDurableAccrual("evil"); !ok {
		t.Fatal("expected durable row before revoke")
	}

	out := wm.revokeWorker("evil", "9.9.9.9", true)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("revoke: %+v", out)
	}
	if _, ok := wm.fetchDurableAccrual("evil"); ok {
		t.Fatal("durable accrual must be deleted on revoke")
	}

	// Submit-path hydrate must not revive unpaid after revoke.
	wm.prefetchWorkerAccrual("evil")
	if _, exists := wm.worker["evil"]; exists {
		t.Fatal("prefetch must not restore revoked worker")
	}

	// Restart path: loadWorkerAccruals must also stay empty for this id.
	wm2 := &workManager{worker: map[string]workerPayoutStat{}}
	wm2.attachDedupDB(db)
	if _, exists := wm2.worker["evil"]; exists {
		t.Fatalf("restart restored revoked accrual: %+v", wm2.worker["evil"])
	}
}

func TestRevokeAbsentWorkerStillDeletesDurable(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual-prune.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{
		worker:  map[string]workerPayoutStat{},
		abuse:   map[string]workerAbuseState{},
		ipAbuse: map[string]workerAbuseState{},
	}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["ghost"] = workerPayoutStat{AcceptedAtt: 1000, PayoutHMC: 1.5}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("ghost", true)
	// Simulate UI prune: memory gone, SQLite still has unpaid.
	wm.mu.Lock()
	delete(wm.worker, "ghost")
	wm.mu.Unlock()

	out := wm.revokeWorker("ghost", "1.1.1.1", true)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("revoke absent: %+v", out)
	}
	if _, ok := wm.fetchDurableAccrual("ghost"); ok {
		t.Fatal("prune+ban must still DELETE durable accrual")
	}
}

func TestPersistWorkerAccrualMonotonic(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual-mono.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{worker: map[string]workerPayoutStat{}}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["w"] = workerPayoutStat{AcceptedAtt: 200, PayoutHMC: 2}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("w", true)

	// Stale write with lower attempts must not clobber.
	wm.mu.Lock()
	wm.worker["w"] = workerPayoutStat{AcceptedAtt: 50, PayoutHMC: 0.5}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("w", true)

	st, ok := wm.fetchDurableAccrual("w")
	if !ok || st.AcceptedAtt != 200 || st.PayoutHMC < 2-1e-9 {
		t.Fatalf("stale overwrite: ok=%v st=%+v", ok, st)
	}
}

// SHARE_STRICT found-bonus can credit payout_units while accepted_attempts stays flat.
// A delayed persist with the same attempts and lower units must not clobber.
func TestAccrualPersistRejectsStaleFoundBonusUnits(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual-found-bonus.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{worker: map[string]workerPayoutStat{}}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["fb"] = workerPayoutStat{AcceptedAtt: 0, PayoutUnits: 5_000_00000, PayoutHMC: 5}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("fb", true)

	wm.mu.Lock()
	wm.worker["fb"] = workerPayoutStat{AcceptedAtt: 0, PayoutUnits: 1_000_00000, PayoutHMC: 1}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("fb", true)

	st, ok := wm.fetchDurableAccrual("fb")
	if !ok || st.PayoutUnits != 5_000_00000 {
		t.Fatalf("stale found-bonus clobber: ok=%v st=%+v", ok, st)
	}

	// Same attempts + higher units must still advance (second found credit).
	wm.mu.Lock()
	wm.worker["fb"] = workerPayoutStat{AcceptedAtt: 0, PayoutUnits: 7_000_00000, PayoutHMC: 7}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("fb", true)
	st, ok = wm.fetchDurableAccrual("fb")
	if !ok || st.PayoutUnits != 7_000_00000 {
		t.Fatalf("units advance: ok=%v st=%+v", ok, st)
	}
}

func TestAccrualJournalDeltaUnits(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual-journal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{worker: map[string]workerPayoutStat{}}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["j"] = workerPayoutStat{AcceptedAtt: 10, PayoutUnits: 1_000_00000, PayoutHMC: 1}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("j", true)

	wm.mu.Lock()
	wm.worker["j"] = workerPayoutStat{AcceptedAtt: 20, PayoutUnits: 1_500_00000, PayoutHMC: 1.5}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("j", true)

	var n int
	var sum uint64
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(delta_units),0) FROM accrual_journal WHERE worker_id='j'`).Scan(&n, &sum); err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("want >=2 journal rows, got %d", n)
	}
	// First persist: +1 HMC; second: +0.5 HMC.
	if sum != 1_500_00000 {
		t.Fatalf("sum delta_units=%d want 150000000", sum)
	}
	st, ok := wm.fetchDurableAccrual("j")
	if !ok || st.PayoutUnits != 1_500_00000 {
		t.Fatalf("watermark: ok=%v st=%+v", ok, st)
	}
}

func TestWorkerAccrualPersistRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{worker: map[string]workerPayoutStat{}}
	wm.attachDedupDB(db)

	wm.mu.Lock()
	wm.worker["worker-kapa-pc"] = workerPayoutStat{
		AcceptedRanges:  3,
		AcceptedHits:    1,
		AcceptedAtt:     12_582_912,
		PayoutHMC:       1.25,
		PayoutSUP:       0.04,
		PayoutAddress:   "HMC-91fe007e4036c602",
		SignedSubmits:   3,
		LastHashrateGHS: 0.18,
		PeakHashrateGHS: 0.22,
		LastSeenUnix:    100,
		LastPoHSeenUnix: 100,
	}
	wm.totalPayoutHMC = 1.25
	wm.totalPayoutSUP = 0.04
	wm.mu.Unlock()
	wm.persistWorkerAccrual("worker-kapa-pc", true)

	wm2 := &workManager{worker: map[string]workerPayoutStat{}}
	wm2.attachDedupDB(db)
	st := wm2.worker["worker-kapa-pc"]
	if st.PayoutHMC < 1.25-1e-9 || st.AcceptedAtt != 12_582_912 {
		t.Fatalf("restored=%+v", st)
	}
	if wm2.totalPayoutHMC < 1.25-1e-9 {
		t.Fatalf("total=%v", wm2.totalPayoutHMC)
	}

	// Simulate prune then prefetch hydrate.
	wm2.mu.Lock()
	wm2.totalPayoutHMC -= st.PayoutHMC
	wm2.totalPayoutSUP -= st.PayoutSUP
	delete(wm2.worker, "worker-kapa-pc")
	wm2.mu.Unlock()
	wm2.prefetchWorkerAccrual("worker-kapa-pc")
	st2 := wm2.worker["worker-kapa-pc"]
	if st2.PayoutHMC < 1.25-1e-9 {
		t.Fatalf("hydrate=%+v", st2)
	}
	if wm2.totalPayoutHMC < 1.25-1e-9 {
		t.Fatalf("total after hydrate=%v", wm2.totalPayoutHMC)
	}
}

func TestStalePersistCannotResurrectAfterRevoke(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{
		worker:       map[string]workerPayoutStat{},
		accrualEpoch: map[string]uint64{},
		abuse:        map[string]workerAbuseState{},
		ipAbuse:      map[string]workerAbuseState{},
	}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["evil"] = workerPayoutStat{
		AcceptedAtt: 5_000_000,
		PayoutHMC:   3,
	}
	wm.mu.Unlock()

	// Simulate TOCTOU: snap under lock, revoke, then finish persist (old path resurrected unpaid).
	wm.mu.Lock()
	st, ok := wm.worker["evil"]
	epoch := wm.accrualEpochLocked("evil")
	wm.mu.Unlock()
	if !ok {
		t.Fatal("expected worker before revoke")
	}

	out := wm.revokeWorker("evil", "9.9.9.9", true)
	if ok, _ := out["ok"].(bool); !ok {
		t.Fatalf("revoke: %+v", out)
	}
	if _, alive := wm.fetchDurableAccrual("evil"); alive {
		t.Fatal("revoke must clear durable before stale write")
	}

	// Stale INSERT as the old goroutine would after unlock.
	_, err = wm.dedupDB.Exec(`
INSERT INTO worker_accrual (
  worker_id, accepted_ranges, accepted_hits, accepted_attempts,
  payout_hmc, payout_sup, payout_address, signed_submits,
  last_ghs, peak_ghs, last_seen_unix, last_poh_seen_unix, last_fuzz_seen_unix, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(worker_id) DO UPDATE SET
  accepted_attempts=excluded.accepted_attempts,
  payout_hmc=excluded.payout_hmc,
  updated_at=excluded.updated_at
WHERE excluded.accepted_attempts >= worker_accrual.accepted_attempts
`, "evil", st.AcceptedRanges, st.AcceptedHits, st.AcceptedAtt,
		st.PayoutHMC, st.PayoutSUP, st.PayoutAddress, st.SignedSubmits,
		st.LastHashrateGHS, st.PeakHashrateGHS, st.LastSeenUnix, st.LastPoHSeenUnix, st.LastFuzzSeenUnix, 1)
	if err != nil {
		t.Fatal(err)
	}
	var rawN int
	_ = wm.dedupDB.QueryRow(`SELECT COUNT(1) FROM worker_accrual WHERE worker_id=?`, "evil").Scan(&rawN)
	if rawN != 1 {
		t.Fatalf("stale INSERT should land a raw row before purge, got %d", rawN)
	}
	// Tombstone hides the row from settle/load even before purge.
	if _, alive := wm.fetchDurableAccrual("evil"); alive {
		t.Fatal("tombstone must hide stale INSERT from fetchDurableAccrual")
	}

	// Fixed persist post-check: epoch mismatch → purge.
	wm.mu.Lock()
	cur := wm.accrualEpochLocked("evil")
	if cur == epoch {
		t.Fatalf("revoke must bump accrual epoch (got %d)", cur)
	}
	wm.deleteWorkerAccrualLocked("evil")
	wm.mu.Unlock()
	if _, alive := wm.fetchDurableAccrual("evil"); alive {
		t.Fatal("epoch mismatch must purge resurrected durable unpaid")
	}

	// End-to-end: persistWorkerAccrual after snap+revoke must also purge.
	wm.mu.Lock()
	wm.worker["evil2"] = workerPayoutStat{AcceptedAtt: 1000, PayoutHMC: 1}
	wm.mu.Unlock()
	started := make(chan struct{})
	blocked := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		wm.mu.Lock()
		_, ok := wm.worker["evil2"]
		epoch2 := wm.accrualEpochLocked("evil2")
		wm.mu.Unlock()
		if !ok {
			return
		}
		close(started)
		<-blocked
		// Finish like persistWorkerAccrual after unlock (force path without gap).
		_, _ = wm.dedupDB.Exec(`
INSERT INTO worker_accrual (
  worker_id, accepted_ranges, accepted_hits, accepted_attempts,
  payout_hmc, payout_sup, payout_address, signed_submits,
  last_ghs, peak_ghs, last_seen_unix, last_poh_seen_unix, last_fuzz_seen_unix, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(worker_id) DO UPDATE SET
  accepted_attempts=excluded.accepted_attempts,
  payout_hmc=excluded.payout_hmc,
  updated_at=excluded.updated_at
`, "evil2", 0, 0, 1000, 1.0, 0.0, "", 0, 0.0, 0.0, 0, 0, 0, 2)
		wm.mu.Lock()
		if wm.accrualEpochLocked("evil2") != epoch2 {
			wm.deleteWorkerAccrualLocked("evil2")
		}
		wm.mu.Unlock()
	}()
	<-started
	_ = wm.revokeWorker("evil2", "1.2.3.4", true)
	close(blocked)
	<-done
	if _, alive := wm.fetchDurableAccrual("evil2"); alive {
		t.Fatal("stale persist after revoke must not leave durable unpaid")
	}
}

func TestPruneDoesNotBumpAccrualEpoch(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual-prune-epoch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{worker: map[string]workerPayoutStat{}, accrualEpoch: map[string]uint64{}}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["keep"] = workerPayoutStat{AcceptedAtt: 100, PayoutHMC: 0.5, LastSeenUnix: 1}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("keep", true)

	wm.mu.Lock()
	before := wm.accrualEpochLocked("keep")
	wm.pruneStaleWorkersLocked("", 10, 1, false, true, 1_000_000)
	after := wm.accrualEpochLocked("keep")
	wm.mu.Unlock()
	if before != after {
		t.Fatalf("prune must not bump epoch (%d → %d)", before, after)
	}
	if _, ok := wm.fetchDurableAccrual("keep"); !ok {
		t.Fatal("prune must leave durable unpaid")
	}
	// persist with worker gone must not delete durable
	wm.persistWorkerAccrual("keep", true)
	if _, ok := wm.fetchDurableAccrual("keep"); !ok {
		t.Fatal("persist after prune must not wipe durable")
	}
}

func TestTombstoneBlocksRestartAfterStaleInsert(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "accrual-tombstone.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	wm := &workManager{
		worker:       map[string]workerPayoutStat{},
		accrualEpoch: map[string]uint64{},
		abuse:        map[string]workerAbuseState{},
		ipAbuse:      map[string]workerAbuseState{},
	}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["evil"] = workerPayoutStat{AcceptedAtt: 9_000_000, PayoutHMC: 5, PayoutUnits: 500_000_000}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("evil", true)

	_ = wm.revokeWorker("evil", "9.9.9.9", true)
	if !wm.accrualRevoked("evil") {
		t.Fatal("revoke must stamp durable tombstone")
	}

	// Crash window: stale INSERT after revoke DELETE (epoch purge never ran).
	_, err = wm.dedupDB.Exec(`
INSERT INTO worker_accrual (
  worker_id, accepted_ranges, accepted_hits, accepted_attempts,
  payout_hmc, payout_units, payout_sup, payout_address, signed_submits,
  last_ghs, peak_ghs, last_seen_unix, last_poh_seen_unix, last_fuzz_seen_unix, updated_at
) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		"evil", 0, 0, 9_000_000, 5.0, 500_000_000, 0.0, "HMC-evil", 0, 0.0, 0.0, 0, 0, 0, 99)
	if err != nil {
		t.Fatal(err)
	}

	// Restart load must skip tombstoned unpaid.
	wm2 := &workManager{worker: map[string]workerPayoutStat{}}
	wm2.attachDedupDB(db)
	if _, exists := wm2.worker["evil"]; exists {
		t.Fatal("restart must not rehydrate tombstoned accrual")
	}
	if _, ok := wm2.fetchDurableAccrual("evil"); ok {
		t.Fatal("fetchDurableAccrual must hide tombstoned rows")
	}

	// persistWorkerAccrual must purge resurrection when tombstone is set.
	wm.mu.Lock()
	wm.worker["evil"] = workerPayoutStat{AcceptedAtt: 9_000_000, PayoutHMC: 5}
	wm.mu.Unlock()
	wm.persistWorkerAccrual("evil", true)
	var n int
	_ = db.QueryRow(`SELECT COUNT(1) FROM worker_accrual WHERE worker_id=?`, "evil").Scan(&n)
	if n != 0 {
		t.Fatalf("persist under tombstone must leave accrual empty, got %d", n)
	}
}
