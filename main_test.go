package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackme/internal/block"
	"hackme/internal/p2p"
)

func TestRequireAdminAuthDisabled(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	req := httptest.NewRequest(http.MethodPost, "/api/genesis", nil)
	rec := httptest.NewRecorder()
	if !requireAdminAuth(rec, req) {
		t.Fatal("expected allow when token unset")
	}
}

func TestRequireAdminAuthMissingToken(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "secret-test-token")
	req := httptest.NewRequest(http.MethodPost, "/api/genesis", nil)
	rec := httptest.NewRecorder()
	if requireAdminAuth(rec, req) {
		t.Fatal("expected deny without header")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestRequireAdminAuthHeaderOK(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "secret-test-token")
	req := httptest.NewRequest(http.MethodPost, "/api/genesis", nil)
	req.Header.Set("X-Hackme-Admin-Token", "secret-test-token")
	rec := httptest.NewRecorder()
	if !requireAdminAuth(rec, req) {
		t.Fatal("expected allow with matching header")
	}
}

func TestRequireAdminAuthBearerOK(t *testing.T) {
	t.Setenv("HACKME_ADMIN_TOKEN", "abc")
	req := httptest.NewRequest(http.MethodPost, "/api/genesis", nil)
	req.Header.Set("Authorization", "Bearer abc")
	rec := httptest.NewRecorder()
	if !requireAdminAuth(rec, req) {
		t.Fatal("expected allow with bearer")
	}
}

func TestBindAddrAllowsBeginnerSolo(t *testing.T) {
	tests := []struct {
		addr string
		ok   bool
	}{
		{"127.0.0.1:8080", true},
		{"localhost:9090", true},
		{"[::1]:8080", true},
		{"0.0.0.0:8080", false},
		{"192.168.1.1:8080", false},
		{"bad", false},
	}
	for _, tc := range tests {
		if got := bindAddrAllowsBeginnerSolo(tc.addr); got != tc.ok {
			t.Fatalf("%q: got %v want %v", tc.addr, got, tc.ok)
		}
	}
}

func TestVerifySyncBlockSignatureUnsignedBlockAllowed(t *testing.T) {
	b := block.NewGenesisBlock("HMC-test-node")
	if err := verifySyncBlockSignature(b); err != nil {
		t.Fatalf("unsigned genesis should be allowed, got: %v", err)
	}
}

func TestVerifySyncBlockSignatureRejectsUnsignedNonGenesis(t *testing.T) {
	t.Setenv("HACKME_P2P_ALLOW_UNSIGNED_SYNC", "")
	b := block.NewPoHBlock(1, "prev", "m", 1, 20, 1, "", "formula")
	if err := verifySyncBlockSignature(b); err == nil {
		t.Fatal("unsigned non-genesis sync block must be rejected by default")
	}
	t.Setenv("HACKME_P2P_ALLOW_UNSIGNED_SYNC", "1")
	if err := verifySyncBlockSignature(b); err != nil {
		t.Fatalf("lab opt-in should allow unsigned: %v", err)
	}
}

func TestVerifySyncBlockSignatureValidSignedBlock(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b := block.NewGenesisBlock("HMC-test-node")
	b.MinerPubKey = hex.EncodeToString(pub)
	b.MinerSig = hex.EncodeToString(ed25519.Sign(priv, []byte(b.Hash)))
	if err := verifySyncBlockSignature(b); err != nil {
		t.Fatalf("expected valid signature, got: %v", err)
	}
}

func TestVerifySyncBlockSignatureRejectsInvalidSignature(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b := block.NewGenesisBlock("HMC-test-node")
	b.MinerPubKey = hex.EncodeToString(pub)
	b.MinerSig = hex.EncodeToString(make([]byte, ed25519.SignatureSize))
	if err := verifySyncBlockSignature(b); err == nil {
		t.Fatal("expected invalid signature rejection")
	}
}

func TestVerifySyncBlockSignatureRejectsUnsupportedAlg(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b := block.NewGenesisBlock("HMC-test-node")
	b.MinerSigAlg = "dilithium3"
	b.MinerPubKey = hex.EncodeToString(pub)
	b.MinerSig = hex.EncodeToString(ed25519.Sign(priv, []byte(b.Hash)))
	if err := verifySyncBlockSignature(b); err == nil {
		t.Fatal("expected unsupported signature algorithm rejection")
	}
}

func TestVerifySyncBlockSignatureLeaderAllowlist(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b := block.NewPoHBlock(1, "prev", "m", 1, 20, 1, "", "formula")
	b.MinerPubKey = hex.EncodeToString(pub)
	b.MinerSig = hex.EncodeToString(ed25519.Sign(priv, []byte(b.Hash)))

	t.Setenv("HACKME_P2P_SYNC_STATE_REPLAY_ENABLED", "1")
	t.Setenv("HACKME_P2P_LEADER_PUBKEYS", "")
	if err := verifySyncBlockSignature(b); err == nil {
		t.Fatal("replay on + empty allowlist must fail")
	}

	t.Setenv("HACKME_P2P_LEADER_PUBKEYS", hex.EncodeToString(other))
	if err := verifySyncBlockSignature(b); err == nil {
		t.Fatal("wrong leader pubkey must fail")
	}

	t.Setenv("HACKME_P2P_LEADER_PUBKEYS", hex.EncodeToString(pub))
	if err := verifySyncBlockSignature(b); err != nil {
		t.Fatalf("allowlisted leader should pass: %v", err)
	}

	t.Setenv("HACKME_P2P_SYNC_STATE_REPLAY_ENABLED", "0")
	t.Setenv("HACKME_P2P_LEADER_PUBKEYS", "")
	if err := verifySyncBlockSignature(b); err != nil {
		t.Fatalf("replay off + empty allowlist should still accept valid sig: %v", err)
	}
}

func TestClientIPFromRemoteAddr(t *testing.T) {
	t.Setenv("HACKME_TRUST_X_FORWARDED_FOR", "0")
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.RemoteAddr = "10.10.10.10:9999"
	if got := clientIP(req); got != "10.10.10.10" {
		t.Fatalf("clientIP=%q", got)
	}
}

func TestClientIPFromXForwardedForWhenTrusted(t *testing.T) {
	t.Setenv("HACKME_TRUST_X_FORWARDED_FOR", "1")
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.RemoteAddr = "127.0.0.1:9999"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	if got := clientIP(req); got != "203.0.113.9" {
		t.Fatalf("clientIP=%q", got)
	}
}

func TestSyncBlockedInfoNoHealthyPeerIsSoftWait(t *testing.T) {
	blocked, code, action := syncBlockedInfo(true, p2p.SyncPullPreview{Reason: "no_lag_or_no_healthy_peer"})
	if blocked {
		t.Fatal("expected soft-wait, got blocked=true")
	}
	if code != "sync_waiting_peer_freshness" {
		t.Fatalf("code=%q", code)
	}
	if action != "wait_and_retry_sync" {
		t.Fatalf("action=%q", action)
	}
}

func TestSyncBlockedInfoForkStaysBlocked(t *testing.T) {
	blocked, code, action := syncBlockedInfo(true, p2p.SyncPullPreview{Reason: "no_direct_tail_match"})
	if !blocked {
		t.Fatal("expected blocked=true for fork")
	}
	if code != "fork_detected_no_reorg_v1" {
		t.Fatalf("code=%q", code)
	}
	if action == "" {
		t.Fatal("expected non-empty action")
	}
}
