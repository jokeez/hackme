package hms

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setupIntegrityCoord(t *testing.T, workers ...string) (*Coordinator, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cfg := Config{
		MinQuotaGB: 10, MaxQuotaGB: 1000,
		EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute,
		InitialSealTarget: defaultSealTarget(), WorkerOnlineSec: 300,
		RepairIntervalSec: 5, HealthSlashStreak: 2,
	}
	coord := NewCoordinator(db, cfg)
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_MARKET_REPLICAS", "2")

	pub := repeatHex(64)
	for _, w := range workers {
		if err := coord.RegisterStorageWorker(w, pub, 100); err != nil {
			t.Fatal(err)
		}
		_ = os.MkdirAll(filepath.Join(dir, "storage", w), 0o755)
	}
	return coord, dir
}

func assertOrderInvariants(t *testing.T, c *Coordinator, orderID string) {
	t.Helper()
	var bytesUploaded int64
	var chunkCount int
	if err := c.db.QueryRow(`SELECT bytes_uploaded, chunk_count FROM hms_orders WHERE order_id=?`, orderID).
		Scan(&bytesUploaded, &chunkCount); err != nil {
		t.Fatalf("order row: %v", err)
	}
	var sumSize int64
	var rowCount int
	if err := c.db.QueryRow(`SELECT COALESCE(SUM(size),0), COUNT(*) FROM hms_order_chunks WHERE order_id=?`, orderID).
		Scan(&sumSize, &rowCount); err != nil {
		t.Fatalf("chunk sum: %v", err)
	}
	if bytesUploaded != sumSize {
		t.Fatalf("bytes_uploaded=%d != sum(chunk.size)=%d", bytesUploaded, sumSize)
	}
	if chunkCount != rowCount {
		t.Fatalf("chunk_count=%d != chunk rows=%d", chunkCount, rowCount)
	}

	rows, err := c.db.Query(`SELECT chunk_index, chunk_id, size, ciphertext_sha256, replica_count FROM hms_order_chunks WHERE order_id=?`, orderID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var idx, replicaCount int
		var chunkID string
		var size int64
		var ctHash []byte
		if err := rows.Scan(&idx, &chunkID, &size, &ctHash, &replicaCount); err != nil {
			t.Fatal(err)
		}
		replicas, err := c.listChunkReplicaWorkers(orderID, idx)
		if err != nil {
			t.Fatal(err)
		}
		if len(replicas) != replicaCount {
			t.Fatalf("chunk %d replica_count=%d rows=%d", idx, replicaCount, len(replicas))
		}
		var laneSize int64
		var laneHash []byte
		err = c.db.QueryRow(`SELECT size, ciphertext_sha256 FROM hms_chunks WHERE chunk_id=?`, chunkID).Scan(&laneSize, &laneHash)
		if err != nil {
			t.Fatalf("hms_chunks missing %s: %v", chunkID, err)
		}
		if laneSize != size || !bytes.Equal(laneHash, ctHash) {
			t.Fatalf("lane chunk metadata drift for %s", chunkID)
		}
		for _, wid := range replicas {
			b, err := c.readMarketChunkFile(wid, chunkID)
			if err != nil {
				continue
			}
			if int64(len(b)) != size {
				t.Fatalf("file size mismatch worker=%s chunk=%s", wid, chunkID)
			}
			sum := sha256.Sum256(b)
			if !bytes.Equal(sum[:], ctHash) {
				t.Fatalf("sha256 mismatch worker=%s chunk=%s", wid, chunkID)
			}
		}
	}
}

func TestIntegrityMultiChunkOrderTotals(t *testing.T) {
	coord, _ := setupIntegrityCoord(t, "w-a", "w-b")
	created, err := coord.CreateStorageOrder("multi", "u1", 1<<20, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	oid := created.Order.OrderID
	tok := created.UploadToken

	for i, payload := range [][]byte{[]byte("chunk-zero-data"), []byte("chunk-one-is-longer")} {
		if _, err := coord.UploadOrderChunk(oid, tok, i, payload); err != nil {
			t.Fatalf("upload %d: %v", i, err)
		}
		assertOrderInvariants(t, coord, oid)
	}
	o, _ := coord.GetStorageOrder(oid)
	if o.ChunkCount != 2 {
		t.Fatalf("chunk_count=%d", o.ChunkCount)
	}
	var expect int64
	for _, p := range [][]byte{[]byte("chunk-zero-data"), []byte("chunk-one-is-longer")} {
		expect += int64(len(p))
	}
	if o.BytesUploaded != expect {
		t.Fatalf("bytes_uploaded=%d want=%d", o.BytesUploaded, expect)
	}
}

func TestIntegrityReuploadSameIndexDoesNotDoubleCount(t *testing.T) {
	coord, _ := setupIntegrityCoord(t, "w-a", "w-b")
	created, err := coord.CreateStorageOrder("reup", "u1", 4096, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	oid := created.Order.OrderID
	tok := created.UploadToken

	first := []byte("original-bytes")
	second := []byte("replacement-is-longer-than-original")

	if _, err := coord.UploadOrderChunk(oid, tok, 0, first); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.UploadOrderChunk(oid, tok, 0, second); err != nil {
		t.Fatal(err)
	}
	assertOrderInvariants(t, coord, oid)

	o, _ := coord.GetStorageOrder(oid)
	if o.BytesUploaded != int64(len(second)) {
		t.Fatalf("bytes_uploaded=%d want=%d", o.BytesUploaded, len(second))
	}
	if o.ChunkCount != 1 {
		t.Fatalf("chunk_count=%d want 1", o.ChunkCount)
	}
	got, _, err := coord.DownloadOrderChunk(oid, tok, 0)
	if err != nil || !bytes.Equal(got, second) {
		t.Fatalf("download after reupload: err=%v got=%q", err, got)
	}
}

func TestIntegrityCapacityDecreasesAfterUpload(t *testing.T) {
	coord, _ := setupIntegrityCoord(t, "w-a", "w-b")
	before, err := coord.NetworkCapacity()
	if err != nil {
		t.Fatal(err)
	}
	created, err := coord.CreateStorageOrder("cap", "u1", 1<<20, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte("x"), 512<<10)
	if _, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, payload); err != nil {
		t.Fatal(err)
	}
	after, err := coord.NetworkCapacity()
	if err != nil {
		t.Fatal(err)
	}
	usedDelta := before.FreeBytes - after.FreeBytes
	// Lane registers one hms_chunks row per chunk (primary worker); replica files are extra.
	if usedDelta < int64(len(payload)) {
		t.Fatalf("free dropped %d want >= %d", usedDelta, len(payload))
	}
}

func TestIntegrityRepairRestoresHealth(t *testing.T) {
	coord, dir := setupIntegrityCoord(t, "w-a", "w-b", "w-c")
	created, err := coord.CreateStorageOrder("repair", "u1", 1<<20, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	oid := created.Order.OrderID
	tok := created.UploadToken
	payload := []byte("repair-me-please")
	out, err := coord.UploadOrderChunk(oid, tok, 0, payload)
	if err != nil {
		t.Fatal(err)
	}
	chunkID := out["chunk_id"].(string)
	_ = os.Remove(filepath.Join(dir, "storage", "w-b", chunkID+".dat"))
	_ = os.Remove(filepath.Join(dir, "market", "w-b", chunkID+".dat"))

	if err := coord.RunHealthTick(); err != nil {
		t.Fatal(err)
	}
	h, _ := coord.OrderHealth(oid)
	if h["health_status"] == HealthFailed {
		t.Fatalf("unexpected failed before repair: %v", h)
	}

	// Mark w-b offline so repair picks w-c.
	stale := time.Now().Unix() - 3600
	_, _ = coord.db.Exec(`UPDATE hms_workers SET last_seen_unix=? WHERE worker_id='w-b'`, stale)
	if err := coord.RunHealthTick(); err != nil {
		t.Fatal(err)
	}
	assertOrderInvariants(t, coord, oid)
	h, _ = coord.OrderHealth(oid)
	if h["restore_ok"] != true {
		t.Fatalf("restore not ok after repair: %v", h)
	}
}

func TestIntegrityAllReplicasLostMarksFailed(t *testing.T) {
	coord, dir := setupIntegrityCoord(t, "w-a", "w-b")
	created, err := coord.CreateStorageOrder("fail", "u1", 4096, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	oid := created.Order.OrderID
	tok := created.UploadToken
	out, err := coord.UploadOrderChunk(oid, tok, 0, []byte(" doomed "))
	if err != nil {
		t.Fatal(err)
	}
	chunkID := out["chunk_id"].(string)
	for _, w := range []string{"w-a", "w-b"} {
		_ = os.Remove(filepath.Join(dir, "storage", w, chunkID+".dat"))
		_ = os.Remove(filepath.Join(dir, "market", w, chunkID+".dat"))
	}
	if err := coord.RunHealthTick(); err != nil {
		t.Fatal(err)
	}
	h, _ := coord.OrderHealth(oid)
	if h["health_status"] != HealthFailed {
		t.Fatalf("want failed health, got %v", h)
	}
	if h["restore_ok"] != false {
		t.Fatal("restore_ok should be false when all replicas gone")
	}
}

func TestIntegrityHTTPQuoteCapacityPreflight(t *testing.T) {
	coord, _ := setupIntegrityCoord(t) // no workers
	mux := http.NewServeMux()
	RegisterHTTP(mux, coord, "", "")

	body := `{"size_bytes":1048576,"retention_days":30}`
	req := httptest.NewRequest(http.MethodPost, "/api/market/quote", bytes.NewBufferString(body))
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("quote without workers: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestIntegrityPaymentIDUnique(t *testing.T) {
	coord, _ := setupIntegrityCoord(t, "w-a", "w-b")
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "0")
	q, err := QuoteStorageOrder(1<<20, 30)
	if err != nil {
		t.Fatal(err)
	}
	pay := "pay-uniq-" + randomHex(4)
	if _, err := coord.CreateStorageOrder("a", "b", 1<<20, 30, q.QuoteHash, pay); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.CreateStorageOrder("c", "d", 1<<20, 30, q.QuoteHash, pay); err == nil {
		t.Fatal("expected duplicate payment_id rejection")
	}
	var n int
	_ = coord.db.QueryRow(`SELECT COUNT(*) FROM hms_orders WHERE payment_id=?`, pay).Scan(&n)
	if n != 1 {
		t.Fatalf("payment_id rows=%d want 1", n)
	}
}

func TestIntegrityChunkIndexBounds(t *testing.T) {
	coord, _ := setupIntegrityCoord(t, "w-a", "w-b")
	created, err := coord.CreateStorageOrder("bounds", "u1", 4096, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, -1, []byte("x"))
	if err == nil {
		t.Fatal("expected reject negative chunk_index")
	}
	_, err = coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 1_000_001, []byte("x"))
	if err == nil {
		t.Fatal("expected reject huge chunk_index")
	}
}

func TestIntegrityStoredSHA256InDB(t *testing.T) {
	coord, _ := setupIntegrityCoord(t, "w-a", "w-b")
	created, err := coord.CreateStorageOrder("hash", "u1", 4096, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("verify-hash-chain")
	out, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, payload)
	if err != nil {
		t.Fatal(err)
	}
	chunkID := out["chunk_id"].(string)
	want := sha256.Sum256(payload)
	var got []byte
	if err := coord.db.QueryRow(`SELECT ciphertext_sha256 FROM hms_order_chunks WHERE chunk_id=?`, chunkID).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != hex.EncodeToString(want[:]) {
		t.Fatalf("order_chunks hash mismatch")
	}
	if out["sha256"].(string) != encodeHex(want[:]) {
		t.Fatalf("response sha256 mismatch")
	}
	assertOrderInvariants(t, coord, created.Order.OrderID)
}
