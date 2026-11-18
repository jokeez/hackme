package main

// Architectural rate-gate invariant (post #27/#28 harden):
//   forgeable declared worker_id must never be charged for rate slots before
//   identity is proven. Release uses check-only identity + IP peer gate;
//   submit charges the worker slot only after signature / payout-lock.

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

func TestHardenReleaseDoesNotChargeClaimBucket(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "harden-rel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pf := &poolfuzz.Service{DB: db}
	wm := &workManager{
		hybridSignerEnabled:  true,
		hybridSignerStrict:   true,
		claimRequirePubKey:   true,
		claimPerMin:          6,
		submitPerMin:         6,
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

	atkPub, atkPriv, _ := ed25519.GenerateKey(nil)
	for i := 0; i < 20; i++ {
		nonce := uint64(i + 1)
		sig := ed25519.Sign(atkPriv, poolfuzz.CanonicalReleaseBytes(poolfuzz.ReleaseSignPayload{
			WorkerID: "unlocked-victim", CampaignID: "c", ItemID: 1, ReleaseNonce: nonce,
		}))
		body, _ := json.Marshal(map[string]any{
			"worker_id": "unlocked-victim", "campaign_id": "c", "item_id": 1,
			"miner_pubkey": hex.EncodeToString(atkPub), "miner_address": signerAddr(atkPub),
			"miner_sig": hex.EncodeToString(sig), "miner_sig_alg": "ed25519", "release_nonce": nonce,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/release", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Hackme-Admin-Token", "worker-tok")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("release flood must not rate-limit via declared id at %d: %s", i, rec.Body.String())
		}
	}
	wm.mu.Lock()
	st := wm.abuse["unlocked-victim"]
	wm.mu.Unlock()
	if st.ClaimCount != 0 {
		t.Fatalf("release must not charge claim bucket, ClaimCount=%d", st.ClaimCount)
	}
}

func TestHardenFailedClaimDoesNotBindPayoutLock(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "harden-claim.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pf := &poolfuzz.Service{DB: db}
	wm := &workManager{
		hybridSignerEnabled: true, hybridSignerStrict: true, claimRequirePubKey: true,
		claimPerMin: 60, submitPerMin: 60, maxWorkers: 100,
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
	atkPub, _, _ := ed25519.GenerateKey(nil)
	body, _ := json.Marshal(map[string]any{
		"worker_id": "never-claimed-rig", "miner_pubkey": hex.EncodeToString(atkPub),
		"miner_address": signerAddr(atkPub), "worker_version": "9.9.9",
		"hunt_harness_exec": "libfuzzer_oneshot",
		"hunt_engines":      "libfuzzer,aflpp,byte",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/work/claim", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "worker-tok")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	// Empty queue → no_fuzz_work; must NOT leave a durable lock.
	if rec.Code == http.StatusOK {
		t.Fatalf("expected no work, got 200: %s", rec.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_payout_lock WHERE worker_id=?`, "never-claimed-rig").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("failed claim must not bind payout lock, rows=%d body=%s", n, rec.Body.String())
	}
}

func TestHardenSubmitPerMinZeroDisablesWorkerCharge(t *testing.T) {
	wm := newTestWorkManagerForPayout(0.005, false)
	if wm.submitPerMin != 0 {
		t.Fatalf("payout harness expects submitPerMin=0, got %d", wm.submitPerMin)
	}
	now := int64(1_700_000_000)
	for i := 0; i < 50; i++ {
		if ok, reason := wm.chargeSubmitWorker("w-ledger-0", now); !ok {
			t.Fatalf("unlimited submitPerMin must not charge-limit: %v %q", ok, reason)
		}
	}
}
