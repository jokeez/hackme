package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

// Regression for the 2026-11-12 block stalls: clearAccrualRevokedLocked runs on every
// credited submit under m.mu. With another writer holding the SQLite write lock, the
// old synchronous DELETE waited out busy_timeout (60s) and froze all claims/submits.
// A worker without a tombstone must now never touch SQLite on that path.
func TestClearAccrualRevokedDoesNotWaitOnBusyDB(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "wedge.db"))
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
	if wm.accrualRevokedMem == nil {
		t.Fatal("tombstone set not loaded at attach")
	}

	// Another writer takes the write lock and keeps it.
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `BEGIN IMMEDIATE`); err != nil {
		t.Fatal(err)
	}
	released := false
	release := func() {
		if !released {
			_, _ = conn.ExecContext(ctx, `ROLLBACK`)
			_ = conn.Close()
			released = true
		}
	}
	t.Cleanup(release)

	start := time.Now()
	wm.mu.Lock()
	wm.clearAccrualRevokedLocked("honest-miner")
	wm.mu.Unlock()
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("submit hot path waited %v on a busy DB for a worker without tombstone", d)
	}
	release()

	// A real tombstone is still written and cleared durably, in order.
	wm.mu.Lock()
	wm.deleteWorkerAccrualLocked("revoked-miner")
	wm.mu.Unlock()
	if !wm.accrualRevoked("revoked-miner") {
		t.Fatal("tombstone not durable")
	}
	wm.mu.Lock()
	wm.clearAccrualRevokedLocked("revoked-miner")
	wm.mu.Unlock()
	if wm.accrualRevoked("revoked-miner") {
		t.Fatal("tombstone not cleared after credited work")
	}
}

// Tombstones written before a restart must be known in memory after attach, or a
// fresh submit would skip clearing them and the worker's accrual stays revoked.
func TestAccrualTombstonesReloadOnAttach(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reload.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	wm := &workManager{worker: map[string]workerPayoutStat{}, abuse: map[string]workerAbuseState{}, ipAbuse: map[string]workerAbuseState{}}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.deleteWorkerAccrualLocked("w1")
	wm.mu.Unlock()
	_ = db.Close()

	db2, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db2.Close() })
	wm2 := &workManager{worker: map[string]workerPayoutStat{}, abuse: map[string]workerAbuseState{}, ipAbuse: map[string]workerAbuseState{}}
	wm2.attachDedupDB(db2)
	if _, ok := wm2.accrualRevokedMem["w1"]; !ok {
		t.Fatal("tombstone w1 not reloaded into memory")
	}
	wm2.mu.Lock()
	wm2.clearAccrualRevokedLocked("w1")
	wm2.mu.Unlock()
	if wm2.accrualRevoked("w1") {
		t.Fatal("reloaded tombstone not cleared")
	}
}

// The root cause: persistWorkerAccrual held the SQLite write lock (BEGIN IMMEDIATE)
// and then took m.mu for its epoch check, while submit held m.mu and waited for the
// SQLite write lock. Each side waited for the other until busy_timeout (60s). With the
// epoch behind its own mutex the persist commits without m.mu, so a DB write made
// under m.mu (here: a revoke) finishes in milliseconds.
func TestPersistAccrualNeverTakesSubmitLockInsideTx(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "inversion.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	wm := &workManager{
		worker:       map[string]workerPayoutStat{},
		abuse:        map[string]workerAbuseState{},
		ipAbuse:      map[string]workerAbuseState{},
		accrualEpoch: map[string]uint64{},
	}
	wm.attachDedupDB(db)
	wm.mu.Lock()
	wm.worker["miner-a"] = workerPayoutStat{AcceptedAtt: 1_000_000, PayoutHMC: 0.5, PayoutAddress: "HMC-00000000000000a1"}
	wm.mu.Unlock()

	inTx := make(chan struct{})
	proceed := make(chan struct{})
	testHookAccrualAfterBegin = func() {
		close(inTx)
		<-proceed
	}
	t.Cleanup(func() { testHookAccrualAfterBegin = nil })

	done := make(chan struct{})
	go func() {
		wm.persistWorkerAccrual("miner-a", true)
		close(done)
	}()
	<-inTx // persist now holds the SQLite write lock

	wm.mu.Lock() // a submit takes m.mu …
	close(proceed)
	start := time.Now()
	wm.deleteWorkerAccrualLocked("miner-b") // … and writes to SQLite under it
	wm.mu.Unlock()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("write under m.mu waited %v behind persistWorkerAccrual (lock inversion)", d)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("persistWorkerAccrual did not finish")
	}
	if _, ok := wm.fetchDurableAccrual("miner-a"); !ok {
		t.Fatal("accrual for miner-a not persisted")
	}
}
