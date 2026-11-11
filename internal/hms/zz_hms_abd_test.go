package hms

import (
	"bytes"
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupABDCoord(t *testing.T, workers ...string) (*Coordinator, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	coord := NewCoordinator(db, Config{
		MinQuotaGB: 10, MaxQuotaGB: 1000,
		EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute,
		InitialSealTarget: defaultSealTarget(), WorkerOnlineSec: 300,
	})
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_COORDINATOR_ALLOW_INSECURE", "1")
	t.Setenv("HMS_MARKET_REPLICAS", "1")
	if len(workers) == 0 {
		workers = []string{"w1"}
	}
	for _, w := range workers {
		if err := coord.RegisterStorageWorker(w, repeatHex(64), 10); err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(filepath.Join(dir, "storage", w), 0o755)
	}
	return coord, dir
}

func chunkFileExists(dir, workerID, chunkID string) bool {
	_, err := os.Stat(filepath.Join(dir, "storage", workerID, chunkID+".dat"))
	return err == nil
}

func TestCancelPurgesChunksAndReclaimsQuota(t *testing.T) {
	coord, dir := setupABDCoord(t, "w1")
	before, err := coord.NetworkCapacity()
	if err != nil {
		t.Fatal(err)
	}
	created, err := coord.CreateStorageOrder("purge", "u", 1<<20, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("p"), minMarketChunkBytes)
	out, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, payload)
	if err != nil {
		t.Fatal(err)
	}
	chunkID := out["chunk_id"].(string)
	if !chunkFileExists(dir, "w1", chunkID) {
		t.Fatal("expected chunk file before cancel")
	}
	afterUp, err := coord.NetworkCapacity()
	if err != nil {
		t.Fatal(err)
	}
	if afterUp.FreeBytes >= before.FreeBytes {
		t.Fatalf("upload did not consume quota: before=%d after=%d", before.FreeBytes, afterUp.FreeBytes)
	}
	cancelled, err := coord.CancelStorageOrder(created.Order.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != OrderStatusCancelled || cancelled.HealthDetail != "cancelled by customer" {
		t.Fatalf("cancel status: %+v", cancelled)
	}
	if cancelled.ChunkCount != 0 || cancelled.BytesUploaded != 0 {
		t.Fatalf("counters not cleared: %+v", cancelled)
	}
	if chunkFileExists(dir, "w1", chunkID) {
		t.Fatal("chunk file survived cancel")
	}
	var nChunks, nLane int
	_ = coord.db.QueryRow(`SELECT COUNT(*) FROM hms_order_chunks WHERE order_id=?`, created.Order.OrderID).Scan(&nChunks)
	_ = coord.db.QueryRow(`SELECT COUNT(*) FROM hms_chunks WHERE chunk_id=?`, chunkID).Scan(&nLane)
	if nChunks != 0 || nLane != 0 {
		t.Fatalf("db rows survived cancel chunks=%d lane=%d", nChunks, nLane)
	}
	after, err := coord.NetworkCapacity()
	if err != nil {
		t.Fatal(err)
	}
	if after.FreeBytes != before.FreeBytes {
		t.Fatalf("quota not reclaimed: before=%d after=%d", before.FreeBytes, after.FreeBytes)
	}
	if _, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, payload); err == nil {
		t.Fatal("upload after purge must fail")
	}
}

func TestRetentionExpiryPurgesLikeCancel(t *testing.T) {
	coord, dir := setupABDCoord(t, "w1")
	before, err := coord.NetworkCapacity()
	if err != nil {
		t.Fatal(err)
	}
	created, err := coord.CreateStorageOrder("expire", "u", 1<<20, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("r"), minMarketChunkBytes)
	out, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, payload)
	if err != nil {
		t.Fatal(err)
	}
	chunkID := out["chunk_id"].(string)
	past := time.Now().Unix() - int64(31*86400)
	if _, err := coord.db.Exec(`UPDATE hms_orders SET created_unix=? WHERE order_id=?`, past, created.Order.OrderID); err != nil {
		t.Fatal(err)
	}
	if err := coord.RunHealthTick(); err != nil {
		t.Fatal(err)
	}
	o, err := coord.GetStorageOrder(created.Order.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if o.Status != OrderStatusExpired || o.HealthDetail != "retention expired" {
		t.Fatalf("expected expired, got %+v", o)
	}
	if chunkFileExists(dir, "w1", chunkID) {
		t.Fatal("chunk file survived retention expiry")
	}
	var nLane int
	_ = coord.db.QueryRow(`SELECT COUNT(*) FROM hms_chunks WHERE chunk_id=?`, chunkID).Scan(&nLane)
	if nLane != 0 || o.BytesUploaded != 0 {
		t.Fatalf("lane=%d uploaded=%d", nLane, o.BytesUploaded)
	}
	h, err := coord.OrderHealth(created.Order.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if h["restore_ok"] != false {
		t.Fatalf("expired order still restorable: %v", h)
	}
	after, err := coord.NetworkCapacity()
	if err != nil {
		t.Fatal(err)
	}
	if after.FreeBytes != before.FreeBytes {
		t.Fatalf("quota not reclaimed on expiry: before=%d after=%d", before.FreeBytes, after.FreeBytes)
	}
	// Second tick stays expired and does not resurrect.
	if err := coord.RunHealthTick(); err != nil {
		t.Fatal(err)
	}
	again, _ := coord.GetStorageOrder(created.Order.OrderID)
	if again.Status != OrderStatusExpired {
		t.Fatalf("status resurrected: %+v", again)
	}
}

func TestChunkIndexDoSBounds(t *testing.T) {
	coord, _ := setupABDCoord(t, "w1")
	created, err := coord.CreateStorageOrder("dos", "u", 1<<20, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	oid := created.Order.OrderID
	tok := created.UploadToken
	tiny := []byte("x")
	if _, err := coord.UploadOrderChunk(oid, tok, 0, tiny); err == nil {
		t.Fatal("expected reject tiny chunk while plan has room for a minimum chunk")
	}
	maxN := maxMarketChunksForPlan(1 << 20)
	if maxN >= 1_000_000 {
		t.Fatalf("max chunks=%d still allows the old 1e6 index DoS", maxN)
	}
	if _, err := coord.UploadOrderChunk(oid, tok, maxN, bytes.Repeat([]byte("z"), minMarketChunkBytes)); err == nil {
		t.Fatal("expected reject chunk_index at max")
	}
	// Tail exception: remaining plan smaller than the minimum.
	small, err := coord.CreateStorageOrder("tail", "u", 100, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.UploadOrderChunk(small.Order.OrderID, small.UploadToken, 0, bytes.Repeat([]byte("t"), 40)); err != nil {
		t.Fatalf("tail chunk should be allowed: %v", err)
	}
	full, err := coord.CreateStorageOrder("fullmin", "u", 1<<20, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.UploadOrderChunk(full.Order.OrderID, full.UploadToken, 0, bytes.Repeat([]byte("m"), minMarketChunkBytes)); err != nil {
		t.Fatalf("minimum chunk should be accepted: %v", err)
	}
}

func TestPickStorageWorkerSkipsFullQuota(t *testing.T) {
	coord, _ := setupABDCoord(t, "w-full", "w-free")
	const gb = int64(1024 * 1024 * 1024)
	now := time.Now().Unix()
	_, err := coord.db.Exec(`INSERT INTO hms_chunks(chunk_id, ciphertext_sha256, size, worker_id, epoch_id, created_unix)
		VALUES('fill-full', X'11', ?, 'w-full', 1, ?)`, 10*gb, now)
	if err != nil {
		t.Fatal(err)
	}
	got, err := coord.PickStorageWorker()
	if err != nil {
		t.Fatal(err)
	}
	if got != "w-free" {
		t.Fatalf("picked %q, want w-free", got)
	}
	many, err := coord.PickStorageWorkers(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(many) != 1 || many[0] != "w-free" {
		t.Fatalf("workers=%v want only w-free", many)
	}
	_, err = coord.db.Exec(`INSERT INTO hms_chunks(chunk_id, ciphertext_sha256, size, worker_id, epoch_id, created_unix)
		VALUES('fill-free', X'22', ?, 'w-free', 1, ?)`, 10*gb, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.PickStorageWorker(); err == nil {
		t.Fatal("expected no worker when every online quota is full")
	}
}

func TestIssueChallengeSkipsTerminalOrderChunks(t *testing.T) {
	coord, dir := setupABDCoord(t, "w1")
	created, err := coord.CreateStorageOrder("chal", "u", 4096, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	out, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, []byte("order-chunk"))
	if err != nil {
		t.Fatal(err)
	}
	orderChunk := out["chunk_id"].(string)
	if _, err := coord.db.Exec(`UPDATE hms_orders SET status=? WHERE order_id=?`, OrderStatusCancelled, created.Order.OrderID); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.IssueChallenge("w1"); err == nil {
		t.Fatal("cancelled order chunk must not be challenged")
	}
	payload := bytes.Repeat([]byte{3}, 64)
	sum := sha256.Sum256(payload)
	if err := coord.AssignChunk("lane-chunk-1", "w1", sum[:], uint64(len(payload)), nil); err != nil {
		t.Fatal(err)
	}
	if err := coord.writeMarketChunkFile("w1", "lane-chunk-1", payload); err != nil {
		t.Fatal(err)
	}
	_ = dir
	ch, err := coord.IssueChallenge("w1")
	if err != nil {
		t.Fatal(err)
	}
	if ch["chunk_id"] != "lane-chunk-1" {
		t.Fatalf("challenged %v, want lane-chunk-1 (order chunk %s is cancelled)", ch["chunk_id"], orderChunk)
	}
	if _, err := coord.db.Exec(`UPDATE hms_orders SET status=? WHERE order_id=?`, OrderStatusFailed, created.Order.OrderID); err != nil {
		t.Fatal(err)
	}
	ch2, err := coord.IssueChallenge("w1")
	if err != nil {
		t.Fatal(err)
	}
	if ch2["chunk_id"] != "lane-chunk-1" {
		t.Fatalf("failed-order chunk leaked into challenge: %v", ch2["chunk_id"])
	}
}
