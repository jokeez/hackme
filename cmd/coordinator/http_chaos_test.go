package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackme/internal/lanpool"
)

func newChaosMux(t *testing.T) (*http.ServeMux, *workManager) {
	t.Helper()
	wm := newHybridTestWorkManager(true, true)
	wm.badStrikesToBan = 2
	wm.banSec = 120
	wm.claimPerMin = 10_000
	wm.submitPerMin = 10_000
	reg := lanpool.NewRegistry()
	mux := http.NewServeMux()
	addWorkRoutes(mux, "", "", true, reg, wm, nil)
	return mux, wm
}

func httpSubmit(mux *http.ServeMux, remoteAddr string, body submitWorkRequest) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/work/submit", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = remoteAddr
	mux.ServeHTTP(rec, req)
	return rec
}

func TestHTTPSubmitReplayForbiddenNoIPBan(t *testing.T) {
	mux, wm := newChaosMux(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const attackerIP = "203.0.113.77:5555"
	base, size, _, _, _, ok, _ := wm.claim("w-http-replay", 0)
	if !ok {
		t.Fatal("claim failed")
	}
	req := submitWorkRequest{
		WorkerID:     "w-http-replay",
		BaseNonce:    base,
		BatchSize:    size,
		WorkID:       buildWorkID("w-http-replay", base, size),
		Attempts:     size,
		SubmitNonce:  100,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req)))
	rec1 := httpSubmit(mux, attackerIP, req)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first submit status=%d body=%s", rec1.Code, rec1.Body.String())
	}
	base2, size2, _, _, _, ok2, _ := wm.claim("w-http-replay", 1000)
	if !ok2 {
		t.Fatal("second claim failed")
	}
	req2 := submitWorkRequest{
		WorkerID:     "w-http-replay",
		BaseNonce:    base2,
		BatchSize:    size2,
		WorkID:       buildWorkID("w-http-replay", base2, size2),
		Attempts:     size2,
		SubmitNonce:  100,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req2.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req2)))
	rec2 := httpSubmit(mux, attackerIP, req2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("replay want 403, got %d body=%s", rec2.Code, rec2.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &body)
	if body["reason"] != "replay" {
		t.Fatalf("replay reason=%v", body["reason"])
	}
	// Shared-key / stale-nonce replay must not temp-ban the client IP (home GPU NAT).
	wm.mu.Lock()
	ipState, inIP := wm.ipAbuse[keyFromRemoteAddr(attackerIP)]
	wm.mu.Unlock()
	if inIP && (ipState.BadStrikes > 0 || ipState.BannedUntil > 0) {
		t.Fatalf("replay must not strike IP, got strikes=%d banned_until=%d", ipState.BadStrikes, ipState.BannedUntil)
	}
}

func TestHTTPSubmitTamperedWalletForbidden(t *testing.T) {
	mux, wm := newChaosMux(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	base, size, _, _, _, ok, _ := wm.claim("w-tamper-http", 0)
	if !ok {
		t.Fatal("claim failed")
	}
	req := submitWorkRequest{
		WorkerID:     "w-tamper-http",
		BaseNonce:    base,
		BatchSize:    size,
		WorkID:       buildWorkID("w-tamper-http", base, size),
		Attempts:     500,
		SubmitNonce:  1,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req)))
	req.MinerAddress = "HMC-ffffffffffffffff" // tamper after sign
	rec := httpSubmit(mux, "198.51.100.9:1234", req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tampered address want 403, got %d body=%s", rec.Code, rec.Body.String())
	}
	st := wm.stats(false)
	if st["signed_submits_rejected"].(uint64) == 0 && rec.Code != http.StatusForbidden {
		t.Fatal("expected rejected signed submit counter or 403")
	}
}

func TestHTTPSubmitInvalidJSON400(t *testing.T) {
	mux, _ := newChaosMux(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/work/submit", bytes.NewReader([]byte("{not json")))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "127.0.0.1:9999"
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json want 400, got %d", rec.Code)
	}
}

func TestSubmitRejectHTTPStatusMapping(t *testing.T) {
	cases := []struct {
		reason string
		want   int
	}{
		{"replay", http.StatusForbidden},
		{"invalid_signature", http.StatusForbidden},
		{"work_id_mismatch", http.StatusBadRequest},
		{"unknown_or_already_closed_range", http.StatusBadRequest},
		{"", http.StatusOK},
		{"claim_rate_limited", http.StatusConflict},
	}
	for _, tc := range cases {
		if got := submitRejectHTTPStatus(tc.reason); got != tc.want {
			t.Fatalf("reason %q: got %d want %d", tc.reason, got, tc.want)
		}
	}
}

func TestHTTPSubmitRecordsIPAbuseFromXForwardedFor(t *testing.T) {
	trustClientForwardedFor = true
	mux, wm := newChaosMux(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	const attackerIP = "203.0.113.88"
	base, size, _, _, _, ok, _ := wm.claim("w-xff-ip", 0)
	if !ok {
		t.Fatal("claim failed")
	}
	req := submitWorkRequest{
		WorkerID:     "w-xff-ip",
		BaseNonce:    base,
		BatchSize:    size,
		WorkID:       buildWorkID("w-xff-ip", base, size),
		Attempts:     size,
		SubmitNonce:  1,
		MinerPubKey:  hex.EncodeToString(pub),
		MinerAddress: signerAddr(pub),
		MinerSigAlg:  "ed25519",
	}
	req.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req)))
	rec := httptest.NewRecorder()
	b, _ := json.Marshal(req)
	httpReq := httptest.NewRequest(http.MethodPost, "/api/work/submit", bytes.NewReader(b))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.RemoteAddr = "127.0.0.1:18081"
	httpReq.Header.Set("X-Forwarded-For", attackerIP)
	mux.ServeHTTP(rec, httpReq)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit status=%d body=%s", rec.Code, rec.Body.String())
	}
	for i := 0; i < 4; i++ {
		bad := req
		bad.SubmitNonce = uint64(10 + i)
		bad.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(bad)))
		rec2 := httptest.NewRecorder()
		b2, _ := json.Marshal(bad)
		r2 := httptest.NewRequest(http.MethodPost, "/api/work/submit", bytes.NewReader(b2))
		r2.Header.Set("Content-Type", "application/json")
		r2.RemoteAddr = "127.0.0.1:18081"
		r2.Header.Set("X-Forwarded-For", attackerIP)
		mux.ServeHTTP(rec2, r2)
	}
	wm.mu.Lock()
	_, inIP := wm.ipAbuse[attackerIP]
	wm.mu.Unlock()
	if !inIP {
		t.Fatal("expected ipAbuse keyed by X-Forwarded-For client IP, not 127.0.0.1")
	}
}

func TestWorkManagerReplayDoesNotStrikeWorkerOrIP(t *testing.T) {
	wm := &workManager{
		defaultBatch:    1000,
		targetMod:       1_000_000,
		leaseSec:        30,
		claimPerMin:     10_000,
		submitPerMin:    10_000,
		badStrikesToBan: 2,
		banSec:          60,
		maxWorkers:      100,
		maxActiveLeases: 100,
		maxDedupEntries: 1000,
		abuse:           make(map[string]workerAbuseState),
		ipAbuse:         make(map[string]workerAbuseState),
		dropReasonCount: make(map[string]uint64),
	}
	now := int64(1_800_000_000)
	const ip = "10.0.0.66"
	wm.markSubmitOutcome("w-replay-ip", ip, "replay", now)
	wm.markSubmitOutcome("w-replay-ip", ip, "replay", now)
	wm.markSubmitOutcome("w-replay-ip", ip, "replay", now)
	if _, in := wm.ipAbuse[ip]; in && wm.ipAbuse[ip].BadStrikes > 0 {
		t.Fatal("replay must not increment ipAbuse strikes")
	}
	if ok, reason := wm.allowSubmit("w-replay-ip", "", now+1); !ok {
		t.Fatalf("worker must not be banned by replay alone, reason=%q", reason)
	}
	if ok, reason := wm.allowSubmit("w-replay-ip", ip, now+1); !ok {
		t.Fatalf("IP must not be banned by replay alone, reason=%q", reason)
	}
}
