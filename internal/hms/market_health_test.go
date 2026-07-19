package hms

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOrderHealthDegradedWhenReplicaMissing(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{
		MinQuotaGB: 10, MaxQuotaGB: 1000,
		EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute,
		InitialSealTarget: defaultSealTarget(), WorkerOnlineSec: 300, RepairIntervalSec: 5,
		HealthSlashStreak: 2,
	}
	coord := NewCoordinator(db, cfg)
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_COORDINATOR_ALLOW_INSECURE", "1")
	t.Setenv("HMS_MARKET_REPLICAS", "2")

	_ = coord.RegisterStorageWorker("w-a", repeatHex(64), 100)
	_ = coord.RegisterStorageWorker("w-b", repeatHex(64), 100)
	_ = os.MkdirAll(filepath.Join(dir, "storage", "w-a"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, "storage", "w-b"), 0o755)

	created, err := coord.CreateStorageOrder("health-test", "c1", 1<<20, 30, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	ct := []byte("encrypted-health-chunk")
	out, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, ct)
	if err != nil {
		t.Fatal(err)
	}
	chunkID := out["chunk_id"].(string)
	_ = os.Remove(filepath.Join(dir, "storage", "w-b", chunkID+".dat"))
	_ = os.Remove(filepath.Join(dir, "market", "w-b", chunkID+".dat"))

	if err := coord.RunHealthTick(); err != nil {
		t.Fatal(err)
	}
	h, err := coord.OrderHealth(created.Order.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if h["health_status"] != HealthDegraded && h["health_status"] != HealthFailed {
		t.Fatalf("expected degraded/failed health, got %v", h)
	}
}

func TestStorageProofFailureSlashesReplica(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{
		MinQuotaGB: 10, MaxQuotaGB: 1000,
		EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute,
		InitialSealTarget: defaultSealTarget(), HealthSlashStreak: 2,
	}
	coord := NewCoordinator(db, cfg)
	now := time.Now().Unix()
	_, _ = db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, seal_target, sealed, payouts_enabled)
		VALUES(1,?,?,?, ?,0,0)`, now-100, now-50, now+500, defaultSealTarget())
	_, _ = db.Exec(`INSERT INTO hms_order_chunks(order_id, chunk_index, chunk_id, worker_id, size, replica_count, created_unix)
		VALUES('ord-x',0,'ord-x-ch0','w-bad',128,1,?)`, now)
	_, _ = db.Exec(`INSERT INTO hms_challenges(challenge_id, epoch_id, worker_id, chunk_id, sector_offset, expected_hash, expires_unix, answered, ok)
		VALUES('ch1',1,'w-bad','ord-x-ch0',0,X'00',?,1,0)`, now+600)

	coord.markStorageChallengeFailed("w-bad", "ord-x-ch0", 1, "ch1")
	coord.recordReplicaHealth("ord-x", 0, "w-bad", false)
	coord.recordReplicaHealth("ord-x", 0, "w-bad", false)

	ok, err := coord.WorkerEligibleForStoragePayout("w-bad")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected slashed worker ineligible for payout")
	}
	eligible, err := coord.WorkerEpochStorageEligible("w-bad", 1)
	if err != nil {
		t.Fatal(err)
	}
	if eligible {
		t.Fatal("expected epoch ineligible after failed proof")
	}
}
