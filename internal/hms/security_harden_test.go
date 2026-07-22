package hms

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRegisterSealWorkerPubkeyImmutable(t *testing.T) {
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", InitialSealTarget: defaultSealTarget()})
	pub1 := strings.Repeat("ab", 32)
	pub2 := strings.Repeat("cd", 32)
	if err := coord.RegisterSealWorker("seal-1", pub1); err != nil {
		t.Fatal(err)
	}
	if err := coord.RegisterSealWorker("seal-1", pub1); err != nil {
		t.Fatalf("same pubkey re-register should succeed: %v", err)
	}
	err = coord.RegisterSealWorker("seal-1", pub2)
	if err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected immutable pubkey error, got %v", err)
	}
}

func TestSubmitSealUsesRegisteredPubkey(t *testing.T) {
	t.Setenv("HMS_STRATUM_INSECURE", "")
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	cfg := Config{PoolID: "hackme-official", FreezeAfter: time.Minute, SealWindow: time.Hour, InitialSealTarget: defaultSealTarget()}
	coord := NewCoordinator(db, cfg)
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubHex := hex.EncodeToString(pub)
	if err := coord.RegisterSealWorker("w1", pubHex); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	var root [32]byte
	root[31] = 1
	easy := bytesFilled(32, 0xff)
	_, _ = db.Exec(`INSERT INTO hms_epochs(epoch_id, started_unix, freeze_unix, seal_end_unix, manifest_root, seal_target, sealed, payouts_enabled)
		VALUES(1,?,?,?,?,?,0,0)`, now-120, now-60, now+3600, root[:], easy)

	p := SealSubmitPayload{WorkerID: "w1", EpochID: 1, Nonce: 7}
	body, _ := json.Marshal(p)
	sig := ed25519.Sign(priv, body)
	attackerPub := strings.Repeat("11", 32)
	if err := coord.SubmitSeal(p, attackerPub, hex.EncodeToString(sig)); err == nil || !strings.Contains(err.Error(), "match") {
		t.Fatalf("attacker pubkey must be rejected, got %v", err)
	}
	if err := coord.SubmitSeal(p, pubHex, hex.EncodeToString(sig)); err != nil {
		t.Fatalf("registered pubkey submit: %v", err)
	}
}

func bytesFilled(n int, b byte) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

func TestStratumBindDefaultsLoopback(t *testing.T) {
	t.Setenv("HMS_STRATUM_HMAC_SECRET", "")
	t.Setenv("HMS_STRATUM_WORKER_HMAC_SECRET", "")
	t.Setenv("HMS_STRATUM_INSECURE", "")
	t.Setenv("HMS_STRATUM_ALLOW_PUBLIC", "")
	if err := stratumBindAllowed("127.0.0.1:3334"); err != nil {
		t.Fatal(err)
	}
	if err := stratumBindAllowed(":3334"); err == nil {
		t.Fatal("bare :port must be rejected without HMAC/ALLOW_PUBLIC")
	}
	t.Setenv("HMS_STRATUM_HMAC_SECRET", "s3cret")
	if err := stratumBindAllowed("0.0.0.0:3334"); err != nil {
		t.Fatalf("HMAC should allow public bind: %v", err)
	}
}

func TestMarketPaymentHMACSecretNoAdminFallback(t *testing.T) {
	t.Setenv("HMS_MARKET_PAYMENT_HMAC_SECRET", "")
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-should-not-count")
	t.Setenv("HMS_COORDINATOR_ADMIN_TOKEN", "coord-should-not-count")
	if MarketPaymentHMACSecret() != "" {
		t.Fatal("admin tokens must not back payment HMAC secret")
	}
	t.Setenv("HMS_MARKET_PAYMENT_HMAC_SECRET", "only-this")
	if MarketPaymentHMACSecret() != "only-this" {
		t.Fatal("dedicated secret not read")
	}
}

func TestPaymentIDUniqueIndex(t *testing.T) {
	t.Setenv("HMS_MARKET_PAYMENT_HMAC_SECRET", "uniq-secret")
	t.Setenv("HMS_COORDINATOR_ALLOW_INSECURE", "")
	dir := t.TempDir()
	db, err := OpenDB(filepath.Join(dir, "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	coord := NewCoordinator(db, Config{PoolID: "p", MinQuotaGB: 1, MaxQuotaGB: 1000, InitialSealTarget: defaultSealTarget(), WorkerOnlineSec: 3600})
	pub := strings.Repeat("ab", 32)
	if err := coord.RegisterStorageWorker("w1", pub, 100); err != nil {
		t.Fatal(err)
	}
	q, err := QuoteStorageOrder(1024, 30)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := SignMarketPaymentProof("pay-unique-1", q.QuoteHash, q.TotalDebitHMC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.CreateStorageOrder("a", "c", 1024, 30, q.QuoteHash, "pay-unique-1", proof, false); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO hms_orders(order_id, label, client_ref, upload_token, size_plan_bytes, status, quote_hash, prepaid_hmc, retention_days, payment_id, created_unix, updated_unix)
		VALUES('ord-dup','l','c','tok',1,'draft',?,?,30,'pay-unique-1',1,1)`, q.QuoteHash, q.TotalDebitHMC)
	if err == nil {
		t.Fatal("expected UNIQUE(payment_id) violation")
	}
}
