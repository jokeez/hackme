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
	addWorkRoutes(mux, "", "", true, reg, wm)
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

func TestHTTPSubmitReplayForbiddenAndIPAbuse(t *testing.T) {
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
	// IP abuse map must record the attacker after crypto abuse strikes.
	for i := 0; i < 3; i++ {
		_, sizeN, _, _, _, okN, _ := wm.claim("w-http-replay", uint64(2000+i)*1000)
		if !okN {
			break
		}
		r := req
		r.BaseNonce = uint64(2000 + i)
		r.BatchSize = sizeN
		r.WorkID = buildWorkID("w-http-replay", r.BaseNonce, sizeN)
		r.SubmitNonce = uint64(200 + i)
		r.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(r)))
		_ = httpSubmit(mux, attackerIP, r)
	}
	wm.mu.Lock()
	ipState, inIP := wm.ipAbuse[keyFromRemoteAddr(attackerIP)]
	wm.mu.Unlock()
	if !inIP || ipState.BadStrikes == 0 {
		t.Fatalf("expected attacker IP in ipAbuse map, got in=%v strikes=%d", inIP, ipState.BadStrikes)
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

func TestWorkManagerReplayStrikesIPNotWorker(t *testing.T) {
	wm := &workManager{
		defaultBatch:    1000,
		targetMod:       1_000_000,
		leaseSec:        30,
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
	if _, in := wm.ipAbuse[ip]; !in || wm.ipAbuse[ip].BadStrikes == 0 {
		t.Fatal("replay must increment ipAbuse strikes after first event")
	}
	if ok, reason := wm.allowSubmit("w-replay-ip", "", now+1); !ok {
		t.Fatalf("worker must not be banned by replay alone, reason=%q", reason)
	}
}
