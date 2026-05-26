package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hackme/internal/poolfuzz"
)

func canonFuzzSign(p poolfuzz.SubmitSignPayload) []byte { return poolfuzz.CanonicalSubmitBytes(p) }

func TestFuzzSubmitTamperedMinerRejected(t *testing.T) {
	wm := &workManager{
		hybridSignerEnabled: true,
		acceptedSubmitNonces: make(map[string]struct{}),
		signedSubmitNonceMax: make(map[string]uint64),
	}
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := poolfuzz.SubmitSignPayload{
		WorkerID: "w-rt", CampaignID: "c1", ItemID: 1, InputN: 1, ActualInput: 2, CheckResult: 0, SubmitNonce: 1,
	}
	body := canonFuzzSign(payload)
	sig := ed25519.Sign(priv, body)
	reqBody, _ := json.Marshal(map[string]any{
		"worker_id": "w-rt", "campaign_id": "c1", "item_id": 1, "input_n": 1, "actual_input": 2,
		"check_result": 0, "submit_nonce": 1,
		"miner_pubkey": hex.EncodeToString(pub),
		"miner_sig":    hex.EncodeToString(sig),
		"miner_address": signerAddr(pub),
	})
	var decoded map[string]any
	_ = json.Unmarshal(reqBody, &decoded)
	decoded["miner_address"] = "HMC-ffffffffffffffff"
	reqBody, _ = json.Marshal(decoded)

	signBody := canonFuzzSign(payload)
	ok, reason, _ := wm.validateFuzzHybridSignature(fuzzSubmitAuth{
		WorkerID: "w-rt", MinerAddress: "HMC-ffffffffffffffff",
		MinerPubKey: hex.EncodeToString(pub), MinerSig: hex.EncodeToString(sig), SubmitNonce: 1,
	}, signBody)
	if ok || reason != "pubkey_address_mismatch" {
		t.Fatalf("tamper: ok=%v reason=%q", ok, reason)
	}
}

func TestFuzzSubmitReplayNonceRejected(t *testing.T) {
	wm := &workManager{
		hybridSignerEnabled:  true,
		acceptedSubmitNonces: make(map[string]struct{}),
		signedSubmitNonceMax: make(map[string]uint64),
	}
	pub, priv, _ := ed25519.GenerateKey(nil)
	payload := poolfuzz.SubmitSignPayload{WorkerID: "w", CampaignID: "c", ItemID: 1, SubmitNonce: 7}
	body := canonFuzzSign(payload)
	sig := hex.EncodeToString(ed25519.Sign(priv, body))
	auth := fuzzSubmitAuth{
		WorkerID: "w", MinerPubKey: hex.EncodeToString(pub), MinerSig: sig, SubmitNonce: 7,
	}
	if ok, _, _ := wm.validateFuzzHybridSignature(auth, body); !ok {
		t.Fatal("first sig should pass")
	}
	if ok, reason, _ := wm.validateFuzzHybridSignature(auth, body); ok || reason != "replay" {
		t.Fatalf("replay want reject, ok=%v reason=%q", ok, reason)
	}
}

func TestHTTPFuzzSubmitUnauth(t *testing.T) {
	pf := &poolfuzz.Service{DB: nil}
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", false, &workManager{}, pf)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/submit", bytes.NewReader([]byte(`{"worker_id":"x"}`)))
	req.Header.Set("Content-Type", "application/json")
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}
