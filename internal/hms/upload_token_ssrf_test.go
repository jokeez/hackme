package hms

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateWorkerEndpointURL_BlocksMetadata(t *testing.T) {
	if _, err := ValidateWorkerEndpointURL("http://169.254.169.254/latest/meta-data"); err == nil {
		t.Fatal("metadata IP must be rejected")
	}
	if _, err := ValidateWorkerEndpointURL("http://user:pass@example.com"); err == nil {
		t.Fatal("userinfo must be rejected")
	}
	if _, err := ValidateWorkerEndpointURL("ftp://example.com"); err == nil {
		t.Fatal("non-http must be rejected")
	}
}

func TestValidateWorkerEndpointURL_PrivateRequiresFlag(t *testing.T) {
	t.Setenv("HMS_ALLOW_PRIVATE_ENDPOINTS", "")
	if _, err := ValidateWorkerEndpointURL("http://10.0.0.5:9090"); err == nil {
		t.Fatal("private endpoint must be rejected without allow flag")
	}
	t.Setenv("HMS_ALLOW_PRIVATE_ENDPOINTS", "1")
	got, err := ValidateWorkerEndpointURL("http://10.0.0.5:9090/")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://10.0.0.5:9090" {
		t.Fatalf("got %q", got)
	}
}

func TestUploadTokenHashAtRestAndLegacyGate(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := Config{MinQuotaGB: 10, MaxQuotaGB: 1000, EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute, InitialSealTarget: defaultSealTarget()}
	coord := NewCoordinator(db, cfg)
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_COORDINATOR_ALLOW_INSECURE", "1")
	t.Setenv("HMS_ALLOW_PRIVATE_ENDPOINTS", "1")
	if err := coord.RegisterStorageWorker("w-tok", repeatHex(64), 50); err != nil {
		t.Fatal(err)
	}
	created, err := coord.CreateStorageOrder("t", "c", 1024, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := db.QueryRow(`SELECT upload_token FROM hms_orders WHERE order_id=?`, created.Order.OrderID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == created.UploadToken {
		t.Fatal("upload token must be hashed at rest")
	}
	if !looksHashedUploadToken(stored) {
		t.Fatalf("expected sha256 hex at rest, got %q", stored)
	}
	if err := coord.verifyUploadToken(created.Order.OrderID, created.UploadToken); err != nil {
		t.Fatal(err)
	}
	if err := coord.verifyUploadToken(created.Order.OrderID, "deadbeef"); err == nil {
		t.Fatal("bad token must fail")
	}

	// Legacy plaintext row requires explicit allow flag.
	_, _ = db.Exec(`UPDATE hms_orders SET upload_token=? WHERE order_id=?`, "legacyplain", created.Order.OrderID)
	t.Setenv("ALLOW_LEGACY_PLAINTEXT", "")
	if err := coord.verifyUploadToken(created.Order.OrderID, "legacyplain"); err == nil {
		t.Fatal("legacy plaintext must be rejected without ALLOW_LEGACY_PLAINTEXT")
	}
	t.Setenv("ALLOW_LEGACY_PLAINTEXT", "1")
	if err := coord.verifyUploadToken(created.Order.OrderID, "legacyplain"); err != nil {
		t.Fatal(err)
	}
}

func TestRemotePushUsesDedicatedTokenNotWorkerToken(t *testing.T) {
	var sawAuth string
	var sawWorkerToken bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		if strings.Contains(sawAuth, "shared-worker-token") {
			sawWorkerToken = true
			http.Error(w, "shared token not allowed", http.StatusUnauthorized)
			return
		}
		if sawAuth != "Bearer push-only-token" {
			http.Error(w, "bad push token", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	t.Setenv("HMS_ALLOW_PRIVATE_ENDPOINTS", "1")
	t.Setenv("HMS_WORKER_PUSH_TOKEN", "push-only-token")
	t.Setenv("HACKME_HMS_WORKER_TOKEN", "shared-worker-token")
	t.Setenv("HMS_MARKET_REQUIRE_REMOTE_PUSH", "1")
	t.Setenv("HMS_MARKET_SKIP_PAYMENT", "1")
	t.Setenv("HMS_COORDINATOR_ALLOW_INSECURE", "1")
	t.Setenv("HMS_MARKET_DATA_DIR", t.TempDir())

	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := Config{MinQuotaGB: 10, MaxQuotaGB: 1000, EpochDuration: time.Hour, FreezeAfter: 50 * time.Minute, SealWindow: 10 * time.Minute, InitialSealTarget: defaultSealTarget()}
	coord := NewCoordinator(db, cfg)
	if err := coord.RegisterStorageWorkerEndpoint("w-push", repeatHex(64), 50, srv.URL); err != nil {
		t.Fatal(err)
	}
	created, err := coord.CreateStorageOrder("t", "c", 1024, 30, "", "", "", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.UploadOrderChunk(created.Order.OrderID, created.UploadToken, 0, []byte("cipher")); err != nil {
		t.Fatal(err)
	}
	if sawWorkerToken {
		t.Fatal("must not forward shared worker token")
	}
	if sawAuth != "Bearer push-only-token" {
		t.Fatalf("auth=%q", sawAuth)
	}
}

func TestMarketPaymentProofBindsCoordinatorID(t *testing.T) {
	t.Setenv("HMS_MARKET_PAYMENT_HMAC_SECRET", "unit-test-secret")
	t.Setenv("HMS_MARKET_COORDINATOR_ID", "coord-a")
	pay := "hmsp-demo-1"
	qh := "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	proof, err := SignMarketPaymentProof(pay, qh, 1.25)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyMarketPaymentProof(pay, qh, proof, 1.25); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HMS_MARKET_COORDINATOR_ID", "coord-b")
	if err := VerifyMarketPaymentProof(pay, qh, proof, 1.25); err == nil {
		t.Fatal("proof must not verify on a different coordinator_id")
	}
}
