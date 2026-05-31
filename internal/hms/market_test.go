package hms

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMarketCreateUploadList(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "hms.db")
	db, err := OpenDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	cfg := Config{MinQuotaGB: 10, MaxQuotaGB: 1000, EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute, InitialSealTarget: defaultSealTarget()}
	coord := NewCoordinator(db, cfg)
	pub := repeatHex(64)
	if err := coord.RegisterStorageWorker("w-market", pub, 100); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	_ = os.MkdirAll(filepath.Join(dir, "storage", "w-market"), 0o755)

	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	created, err := coord.CreateStorageOrder("acme-backup", "client:acme", 1<<20, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if created.UploadToken == "" {
		t.Fatal("missing token")
	}

	ct := []byte("encrypted-chunk-payload-12345")
	out, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, ct)
	if err != nil {
		t.Fatal(err)
	}
	if out["chunk_id"] == "" {
		t.Fatal("no chunk_id")
	}

	p := filepath.Join(dir, "storage", "w-market", out["chunk_id"].(string)+".dat")
	if st, err := os.Stat(p); err != nil || st.Size() != int64(len(ct)) {
		t.Fatalf("worker drop file: %v size=%d", err, st)
	}

	list, err := coord.ListStorageOrders(10)
	if err != nil || len(list) != 1 {
		t.Fatalf("list: %v n=%d", err, len(list))
	}
	if list[0].BytesUploaded != int64(len(ct)) || list[0].ChunkCount != 1 {
		t.Fatalf("order stats: %+v", list[0])
	}

	st := coord.MarketStats()
	if st["orders_total"].(int) != 1 {
		t.Fatalf("stats: %v", st)
	}
}

func TestMarketHTTPFlow(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := Config{MinQuotaGB: 10, MaxQuotaGB: 1000, EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute, InitialSealTarget: defaultSealTarget()}
	coord := NewCoordinator(db, cfg)
	_ = coord.RegisterStorageWorker("w1", repeatHex(32), 50)
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "stor"))
	_ = os.MkdirAll(filepath.Join(dir, "stor", "w1"), 0o755)

	mux := http.NewServeMux()
	RegisterHTTP(mux, coord, "", "")
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")

	// Create
	body := `{"label":"test","client_ref":"u1","size_plan_bytes":4096,"retention_days":30}`
	req := httptest.NewRequest(http.MethodPost, "/api/market/orders", bytes.NewBufferString(body))
	req.RemoteAddr = "127.0.0.1:1234"
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var createResp struct {
		Status string `json:"status"`
		Result struct {
			Order       StorageOrder `json:"order"`
			UploadToken string       `json:"upload_token"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatal(err)
	}
	oid := createResp.Result.Order.OrderID
	tok := createResp.Result.UploadToken

	// Upload JSON
	upBody, _ := json.Marshal(map[string]any{
		"chunk_index":    0,
		"ciphertext_hex": "deadbeef",
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/market/orders/"+oid+"/upload", bytes.NewReader(upBody))
	req2.RemoteAddr = "127.0.0.1:1234"
	req2.Header.Set("X-HMS-Upload-Token", tok)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec2.Code, rec2.Body.String())
	}

	// List
	req3 := httptest.NewRequest(http.MethodGet, "/api/market/orders", nil)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("list: %d", rec3.Code)
	}

	// Stats
	req4 := httptest.NewRequest(http.MethodGet, "/api/market/stats", nil)
	rec4 := httptest.NewRecorder()
	mux.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("stats: %d", rec4.Code)
	}
}

func TestMarketUploadRequiresToken(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{MinQuotaGB: 10, MaxQuotaGB: 100, EpochDuration: time.Hour, FreezeAfter: time.Hour, SealWindow: time.Minute, InitialSealTarget: defaultSealTarget()})
	_ = coord.RegisterStorageWorker("w1", repeatHex(32), 50)
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	_ = os.MkdirAll(filepath.Join(dir, "storage", "w1"), 0o755)
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	created, err := coord.CreateStorageOrder("x", "y", 1<<30, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = coord.UploadOrderChunk(created.Order.OrderID, "wrong", 0, []byte("data"))
	if err == nil {
		t.Fatal("expected token error")
	}
}

func TestMarketUploadExceedsSizePlan(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{MinQuotaGB: 10, MaxQuotaGB: 100, EpochDuration: time.Hour, FreezeAfter: time.Hour, SealWindow: time.Minute, InitialSealTarget: defaultSealTarget()})
	_ = coord.RegisterStorageWorker("w1", repeatHex(32), 50)
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	_ = os.MkdirAll(filepath.Join(dir, "storage", "w1"), 0o755)
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")

	created, err := coord.CreateStorageOrder("cap", "u", 100, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	oid := created.Order.OrderID
	tok := created.UploadToken

	if _, err := coord.UploadOrderChunk(oid, tok, 0, bytes.Repeat([]byte("a"), 60)); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.UploadOrderChunk(oid, tok, 1, bytes.Repeat([]byte("b"), 50)); err == nil {
		t.Fatal("expected upload exceeds plan error")
	}
}

func TestMarketDownloadRequiresToken(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{MinQuotaGB: 10, MaxQuotaGB: 100, EpochDuration: time.Hour, FreezeAfter: time.Hour, SealWindow: time.Minute, InitialSealTarget: defaultSealTarget()})
	_ = coord.RegisterStorageWorker("w1", repeatHex(32), 50)
	t.Setenv("HMS_MARKET_DATA_DIR", filepath.Join(dir, "market"))
	t.Setenv("HMS_MARKET_STORAGE_ROOT", filepath.Join(dir, "storage"))
	_ = os.MkdirAll(filepath.Join(dir, "storage", "w1"), 0o755)
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")

	created, err := coord.CreateStorageOrder("dl", "u", 4096, 30, "", "")
	if err != nil {
		t.Fatal(err)
	}
	oid := created.Order.OrderID
	tok := created.UploadToken
	ct := []byte("secret-ciphertext")
	if _, err := coord.UploadOrderChunk(oid, tok, 0, ct); err != nil {
		t.Fatal(err)
	}
	if _, _, err := coord.DownloadOrderChunk(oid, "wrong-token", 0); err == nil {
		t.Fatal("expected download token error")
	}
	if _, _, err := coord.DownloadOrderChunk(oid, "", 0); err == nil {
		t.Fatal("expected missing token error")
	}
	got, _, err := coord.DownloadOrderChunk(oid, tok, 0)
	if err != nil || !bytes.Equal(got, ct) {
		t.Fatalf("download: err=%v got=%q want=%q", err, got, ct)
	}
}

func TestMarketPaymentReplayBlocked(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{MinQuotaGB: 10, MaxQuotaGB: 100, EpochDuration: time.Hour, FreezeAfter: time.Hour, SealWindow: time.Minute, InitialSealTarget: defaultSealTarget()})
	if err := coord.RegisterStorageWorker("w-pay", repeatHex(64), 50); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "0")
	q, err := QuoteStorageOrder(1<<20, 30)
	if err != nil {
		t.Fatal(err)
	}
	payID := "pay-test-" + randomHex(4)
	if _, err := coord.CreateStorageOrder("a", "b", 1<<20, 30, q.QuoteHash, payID); err != nil {
		t.Fatal(err)
	}
	if _, err := coord.CreateStorageOrder("c", "d", 1<<20, 30, q.QuoteHash, payID); err == nil {
		t.Fatal("expected payment_id replay error")
	}
}

func TestMarketQuoteTamperRejected(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "hms.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{MinQuotaGB: 10, MaxQuotaGB: 100, EpochDuration: time.Hour, FreezeAfter: time.Hour, SealWindow: time.Minute, InitialSealTarget: defaultSealTarget()})
	if err := coord.RegisterStorageWorker("w-tamper", repeatHex(64), 50); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "0")
	if _, err := coord.CreateStorageOrder("x", "y", 1<<20, 30, "deadbeef", "pay-1"); err == nil {
		t.Fatal("expected quote tamper error")
	}
}

func repeatHex(n int) string {
	const h = "0123456789abcdef"
	b := make([]byte, n)
	for i := range b {
		b[i] = h[i%16]
	}
	return string(b)
}
