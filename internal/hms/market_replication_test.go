package hms

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarketUploadDuringEpochFreeze(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{
		MinQuotaGB: 10, MaxQuotaGB: 100,
		EpochDuration:     30 * time.Second,
		FreezeAfter:       1 * time.Second,
		SealWindow:        5 * time.Second,
		InitialSealTarget: defaultSealTarget(),
	}
	coord := NewCoordinator(db, cfg)
	if err := coord.RegisterStorageWorker("w1", repeatHex(32), 50); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_MARKET_REPLICAS", "1")
	_ = os.MkdirAll(filepath.Join(dir, "storage", "w1"), 0o755)

	created, err := coord.CreateStorageOrder("freeze", "u", 4096, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ct := []byte("upload-while-frozen")
	out, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, ct)
	if err != nil {
		t.Fatalf("market upload during seal freeze: %v", err)
	}
	if out["replica_count"].(int) != 1 {
		t.Fatalf("replicas: %v", out["replica_count"])
	}
	_, _ = db.Exec(`UPDATE hms_epochs SET freeze_unix=0 WHERE epoch_id=(SELECT MAX(epoch_id) FROM hms_epochs)`)

	// Seal-lane ingest still frozen for non-market chunk ids.
	if err := coord.AssignChunk("worker-chunk-1", "w1", bytes.Repeat([]byte("a"), 32), 32, nil); err == nil {
		t.Fatal("expected seal AssignChunk blocked when epoch frozen")
	}
}

func TestMarketChunkReplication(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	coord := NewCoordinator(db, Config{
		MinQuotaGB: 10, MaxQuotaGB: 1000,
		EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute,
		InitialSealTarget: defaultSealTarget(),
	})
	_ = coord.RegisterStorageWorker("w-a", repeatHex(32), 100)
	_ = coord.RegisterStorageWorker("w-b", repeatHex(32), 100)
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_MARKET_REPLICAS", "2")
	for _, w := range []string{"w-a", "w-b"} {
		_ = os.MkdirAll(filepath.Join(dir, "storage", w), 0o755)
	}

	created, err := coord.CreateStorageOrder("repl", "u", 8192, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	ct := []byte("replicated-ciphertext")
	out, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, ct)
	if err != nil {
		t.Fatal(err)
	}
	if out["replica_count"].(int) != 2 {
		t.Fatalf("expected 2 replicas, got %v", out["replica_count"])
	}
	hosts, _ := out["replica_hosts"].([]string)
	if len(hosts) != 2 {
		t.Fatalf("hosts: %v", out["replica_hosts"])
	}
	for _, h := range hosts {
		p := filepath.Join(dir, "storage", h, out["chunk_id"].(string)+".dat")
		if st, err := os.Stat(p); err != nil || st.Size() != int64(len(ct)) {
			t.Fatalf("missing replica file on %s: %v", h, err)
		}
	}
}
