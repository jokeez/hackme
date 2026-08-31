package hms

import (
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSealHashMatchesSPECSingleSHA(t *testing.T) {
	var root [32]byte
	root[0] = 0xab
	got := SealHash(7, root, "pool", 99)

	h := sha256.New()
	var e [8]byte
	e[7] = 7
	_, _ = h.Write(e[:])
	_, _ = h.Write(root[:])
	_, _ = h.Write([]byte("pool"))
	var n [8]byte
	n[7] = 99
	_, _ = h.Write(n[:])
	want := h.Sum(nil)
	if got != *(*[32]byte)(want) {
		t.Fatalf("SealHash must be single SHA256 of preimage (got double-hash previously)")
	}
}

func TestSubmitSealRequiresSignatureByDefault(t *testing.T) {
	t.Setenv("HMS_STRATUM_INSECURE", "")
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := Config{PoolID: "hackme-official", FreezeAfter: time.Minute, SealWindow: time.Hour, InitialSealTarget: defaultSealTarget()}
	coord := NewCoordinator(db, cfg)
	now := time.Now().Unix()
	var root [32]byte
	root[31] = 1
	_, _ = db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, manifest_root, seal_target, sealed, payouts_enabled)
		VALUES(1,?,?,?,?,?,0,0)`, now-120, now-60, now+3600, root[:], defaultSealTarget())
	err = coord.SubmitSeal(SealSubmitPayload{WorkerID: "w", EpochID: 1, Nonce: 1}, "", "")
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("expected signature required, got %v", err)
	}
}

func TestMarketOrdersRequireAuth(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget()})
	mux := http.NewServeMux()
	RegisterHTTP(mux, coord, "admin-secret", "worker-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/market/orders", nil)
	req.RemoteAddr = "198.51.100.10:5555"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous list must 401: status=%d body=%s", rr.Code, rr.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/market/orders", nil)
	req2.RemoteAddr = "198.51.100.10:5555"
	req2.Header.Set("Authorization", "Bearer admin-secret")
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("admin list must 200: status=%d body=%s", rr2.Code, rr2.Body.String())
	}
}

func TestMarketOrderDetailRequiresAuth(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget()})
	mux := http.NewServeMux()
	RegisterHTTP(mux, coord, "admin-secret", "worker-secret")

	for _, path := range []string{
		"/api/market/orders/ord-1/health",
		"/api/market/orders/ord-1/chunks",
		"/api/market/orders/ord-1",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.RemoteAddr = "198.51.100.10:5555"
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("%s anonymous must 401: status=%d", path, rr.Code)
		}
	}
}

func TestSealPayoutsRequireAuth(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget()})
	mux := http.NewServeMux()
	RegisterHTTP(mux, coord, "admin-secret", "worker-secret")

	req := httptest.NewRequest(http.MethodGet, "/api/seal/payouts?epoch_id=1", nil)
	req.RemoteAddr = "198.51.100.10:5555"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous seal payouts must 401: status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAbuseGuardIgnoresSpoofedXFF(t *testing.T) {
	InitClientIPTrust("0.0.0.0:18082")
	g := NewAbuseGuard(1, time.Minute)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.RemoteAddr = "198.51.100.10:5555"
	if !g.AllowHTTP(req, "w1") {
		t.Fatal("first request from real IP should pass")
	}
	if g.AllowHTTP(req, "w1") {
		t.Fatal("second request from same real IP should hit rate limit, not spoofed XFF")
	}
}

func TestAuthRejectsXForwardedForSpoof(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget()})
	mux := http.NewServeMux()
	// Empty tokens: only RemoteAddr loopback authenticates (not X-Forwarded-For).
	RegisterHTTP(mux, coord, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/storage/register", strings.NewReader(
		`{"worker_id":"w1","pubkey_hex":"`+strings.Repeat("ab", 32)+`","quota_gb":50}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	req.RemoteAddr = "198.51.100.10:5555"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("spoofed XFF should not authenticate: status=%d body=%s", rr.Code, rr.Body.String())
	}
}

func TestAuthLoopbackEmptyTokensAllowed(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget()})
	mux := http.NewServeMux()
	RegisterHTTP(mux, coord, "", "")

	req := httptest.NewRequest(http.MethodPost, "/api/storage/register", strings.NewReader(
		`{"worker_id":"w1","pubkey_hex":"`+strings.Repeat("ab", 32)+`","quota_gb":50}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:5555"
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("loopback empty tokens should auth: %d %s", rr.Code, rr.Body.String())
	}
}

func TestRejectPathTraversalWorkerID(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget()})
	err = coord.RegisterStorageWorker("../evil", strings.Repeat("ab", 32), 50)
	if err == nil {
		t.Fatal("expected traversal reject")
	}
	err = coord.RegisterStorageWorker("ok/../no", strings.Repeat("ab", 32), 50)
	if err == nil {
		t.Fatal("expected slash reject")
	}
	if p := filepathJoinMarket(dir, "../evil", "c.dat"); p != "" {
		t.Fatalf("path join must refuse traversal, got %q", p)
	}
}

func TestRejectPathTraversalChunkID(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().Unix()
	_, _ = db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, seal_target, sealed, payouts_enabled)
		VALUES(1,?,?,?,?,0,0)`, now-10, now+300, now+600, defaultSealTarget())
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget()})
	if err := coord.RegisterStorageWorker("ok-w", strings.Repeat("ab", 32), 50); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("x"))
	err = coord.AssignMarketChunk("../evil", "ok-w", sum[:], 1, nil)
	if err == nil {
		t.Fatal("expected chunk traversal reject")
	}
	err = coord.AssignMarketChunk("ok/evil", "ok-w", sum[:], 1, nil)
	if err == nil {
		t.Fatal("expected slash reject")
	}
}

func TestPaymentProofRequiredWhenSecretSet(t *testing.T) {
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "0")
	t.Setenv("HMS_MARKET_PAYMENT_HMAC_SECRET", "test-secret-xyz")
	t.Setenv("HACKME_HMS_COORDINATOR_TOKEN", "")
	t.Setenv("HMS_COORDINATOR_ADMIN_TOKEN", "")
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget()})
	if err := coord.RegisterStorageWorker("w-pay", strings.Repeat("ab", 32), 100); err != nil {
		t.Fatal(err)
	}
	q, err := QuoteStorageOrder(1<<20, 30)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.CreateStorageOrder("x", "y", 1<<20, 30, q.QuoteHash, "pay-1", "", false); err == nil {
		t.Fatal("expected payment_proof required")
	}
	proof, err := SignMarketPaymentProof("pay-1", q.QuoteHash, q.TotalDebitHMC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.CreateStorageOrder("x", "y", 1<<20, 30, q.QuoteHash, "pay-1", proof, false); err != nil {
		t.Fatal(err)
	}
}
