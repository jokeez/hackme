package main

// AZ 2026-11-15: /api/fuzz/work/release must require ed25519 PoP under hybrid strict.
// Unsigned pubkey alone must not free (or attempt to free) another worker's lease.

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func TestAZReleaseRequiresProofOfPossession(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "az-rel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pf := &poolfuzz.Service{DB: db}
	wm := &workManager{
		hybridSignerEnabled:  true,
		hybridSignerStrict:   true,
		claimRequirePubKey:   true,
		claimPerMin:          60,
		submitPerMin:         60,
		acceptedSubmitNonces: make(map[string]struct{}),
		signedSubmitNonceMax: make(map[string]uint64),
		abuse:                make(map[string]workerAbuseState),
		ipAbuse:              make(map[string]workerAbuseState),
		worker:               map[string]workerPayoutStat{},
		dropReasonCount:      make(map[string]uint64),
	}
	wm.attachDedupDB(db)
	mux := http.NewServeMux()
	addFuzzPoolRoutes(mux, "admin-tok", "worker-tok", "", false, wm, pf)

	pub, priv, _ := ed25519.GenerateKey(nil)
	unsigned, _ := json.Marshal(map[string]any{
		"worker_id": "w-az", "campaign_id": "c", "item_id": 1,
		"miner_pubkey": hex.EncodeToString(pub), "miner_address": signerAddr(pub),
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/release", bytes.NewReader(unsigned))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "worker-tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unsigned release: want 403, got %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	reason, _ := out["reason"].(string)
	if reason != "signature_required" && reason != "missing_signature_fields" {
		t.Fatalf("unsigned release reason=%q want signature_required|missing_signature_fields", reason)
	}

	nonce := uint64(42)
	sig := ed25519.Sign(priv, poolfuzz.CanonicalReleaseBytes(poolfuzz.ReleaseSignPayload{
		WorkerID: "w-az", CampaignID: "c", ItemID: 1, ReleaseNonce: nonce,
	}))
	signed, _ := json.Marshal(map[string]any{
		"worker_id": "w-az", "campaign_id": "c", "item_id": 1,
		"miner_pubkey": hex.EncodeToString(pub), "miner_address": signerAddr(pub),
		"miner_sig": hex.EncodeToString(sig), "miner_sig_alg": "ed25519", "release_nonce": nonce,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/release", bytes.NewReader(signed))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-Hackme-Admin-Token", "worker-tok")
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	// No lease held → lease_not_held (proves PoP passed).
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("signed release: want 403 lease_not_held, got %d %s", rec2.Code, rec2.Body.String())
	}
	var out2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &out2)
	if out2["reason"] != "lease_not_held" {
		t.Fatalf("signed release reason=%v want lease_not_held", out2["reason"])
	}
}
