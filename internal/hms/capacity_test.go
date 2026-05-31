package hms

import (
	"path/filepath"
	"testing"
	"time"
)

func TestNetworkCapacityExcludesOfflineWorkers(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{
		MinQuotaGB: 10, MaxQuotaGB: 1000,
		EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute,
		InitialSealTarget: defaultSealTarget(), WorkerOnlineSec: 300,
	}
	coord := NewCoordinator(db, cfg)
	now := time.Now().Unix()
	_, err = db.Exec(`INSERT INTO hms_workers(worker_id, role, pubkey_hex, quota_gb, last_seen_unix, created_unix)
		VALUES('w-online','storage','aa',100,?,?), ('w-stale','storage','bb',200,?,?)`,
		now, now, now-3600, now-3600)
	if err != nil {
		t.Fatal(err)
	}

	snap, err := coord.NetworkCapacity()
	if err != nil {
		t.Fatal(err)
	}
	if snap.OnlineWorkers != 1 || snap.TotalWorkers != 2 {
		t.Fatalf("online=%d total=%d", snap.OnlineWorkers, snap.TotalWorkers)
	}
	const gb = int64(1024 * 1024 * 1024)
	if snap.TotalQuotaBytes != 100*gb {
		t.Fatalf("quota bytes=%d want %d", snap.TotalQuotaBytes, 100*gb)
	}
}

func TestEnsureCapacityPreflight(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{
		MinQuotaGB: 10, MaxQuotaGB: 1000,
		EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute,
		InitialSealTarget: defaultSealTarget(), WorkerOnlineSec: 300,
	}
	coord := NewCoordinator(db, cfg)
	if err := coord.RegisterStorageWorker("w1", repeatHex(64), 10); err != nil {
		t.Fatal(err)
	}

	_, err = coord.EnsureCapacity(RequiredCapacityBytes(6 << 30))
	if !isInsufficientCapacity(err) {
		t.Fatalf("expected insufficient capacity, got %v", err)
	}

	_, err = coord.EnsureCapacity(1 << 20)
	if err != nil {
		t.Fatalf("small order should fit: %v", err)
	}
}

func TestRegisterStorageWorkerRejectsStaleQuotaBump(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{
		MinQuotaGB: 10, MaxQuotaGB: 1000,
		EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute,
		InitialSealTarget: defaultSealTarget(), WorkerOnlineSec: 60,
	}
	coord := NewCoordinator(db, cfg)
	if err := coord.RegisterStorageWorker("w1", repeatHex(64), 50); err != nil {
		t.Fatal(err)
	}
	stale := time.Now().Unix() - 120
	_, _ = db.Exec(`UPDATE hms_workers SET last_seen_unix=? WHERE worker_id='w1'`, stale)
	if err := coord.RegisterStorageWorker("w1", repeatHex(64), 80); err == nil {
		t.Fatal("expected quota update rejection for offline worker")
	}
}
