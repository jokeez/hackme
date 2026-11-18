package main

// Report #27: payout-lock poisoning via signatureless release (and related
// recovery). Release must CHECK an existing lock, never CREATE one on a
// failed/no-op release. Admin unbind clears durable + in-memory rows.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/hunt"
	"hackme/internal/poolfuzz"
	"hackme/internal/store"
	"hackme/internal/workerid"
)

func report27Setup(t *testing.T) (*sql.DB, *workManager, *http.ServeMux) {
	t.Helper()
	t.Setenv("HACKME_POOL_HUNT_REPLAY", "1")
	t.Setenv("HACKME_POOL_HUNT_REPLAY_ASYNC", "1")
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "r27.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	pf := &poolfuzz.Service{DB: db}
	cfg := map[string]any{
		"pool_distributed":     true,
		"work_kind":            "hunt_shard",
		"campaign_type":        "hunt",
		"upstream_target_id":   "jsmn",
		"harness_hash":         "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef",
		"iterations_per_shard": 2,
		"max_input_bytes":      256,
		"depth_tier":           "oss_cve",
		"check_semantics":      "native_crash",
	}
	if err := pf.RegisterCampaign(ctx, poolfuzz.Campaign{ID: "r27-camp", CampaignType: "hunt", Status: "running", BudgetRuns: 4, Config: cfg}); err != nil {
		t.Fatal(err)
	}
	hunt.SetHarnessObjectDir("")
	t.Cleanup(func() { hunt.SetHarnessObjectDir("") })
	if err := hunt.PutHarnessArtifact(ctx, db, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef", []byte("r27-harness-blob"), "jsmn"); err != nil {
		t.Fatal(err)
	}
	_ = pf.EnsureWorkItems(ctx, "r27-camp", time.Now().Unix())
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
	addWorkRoutes(mux, "admin-tok", "worker-tok", false, nil, wm, nil)
	return db, wm, mux
}

func report27Post(t *testing.T, mux *http.ServeMux, path string, body map[string]any, admin bool) (int, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if admin {
		req.Header.Set("X-Hackme-Admin-Token", "admin-tok")
	} else {
		req.Header.Set("X-Hackme-Admin-Token", "worker-tok")
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// Variant A fixed: failed release under a not-yet-locked id must NOT write a durable lock.
func TestReport27ReleaseLaneDoesNotPoisonPayoutLock(t *testing.T) {
	db, _, mux := report27Setup(t)
	atkPub, atkPriv, _ := ed25519.GenerateKey(nil)
	vicPub, _, _ := ed25519.GenerateKey(nil)

	relNonce := uint64(1)
	relSig := ed25519.Sign(atkPriv, poolfuzz.CanonicalReleaseBytes(poolfuzz.ReleaseSignPayload{
		WorkerID: "victim-rig-01", CampaignID: "r27-camp", ItemID: 1, ReleaseNonce: relNonce,
	}))
	code, out := report27Post(t, mux, "/api/fuzz/work/release", map[string]any{
		"worker_id": "victim-rig-01", "campaign_id": "r27-camp", "item_id": 1,
		"miner_pubkey": hex.EncodeToString(atkPub), "miner_address": signerAddr(atkPub),
		"miner_sig": hex.EncodeToString(relSig), "miner_sig_alg": "ed25519", "release_nonce": relNonce,
	}, false)
	if code != http.StatusForbidden || out["reason"] != "lease_not_held" {
		t.Fatalf("release: want 403 lease_not_held, got %d %v", code, out)
	}

	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_payout_lock WHERE worker_id=?`, "victim-rig-01").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("release must not create payout lock, rows=%d", n)
	}

	code, out = report27Post(t, mux, "/api/fuzz/work/claim", map[string]any{
		"worker_id":      "victim-rig-01",
		"miner_pubkey":   hex.EncodeToString(vicPub),
		"miner_address":  signerAddr(vicPub),
		"worker_version": "9.9.9", "hunt_harness_exec": "libfuzzer_oneshot",
		"hunt_engines": "libfuzzer,aflpp,byte",
	}, false)
	if code != http.StatusOK {
		t.Fatalf("victim first claim must succeed after non-poisoning release: %d %v", code, out)
	}

	code, out = report27Post(t, mux, "/api/work/claim", map[string]any{
		"worker_id":            "victim-rig-01b",
		"miner_pubkey":         hex.EncodeToString(vicPub),
		"miner_pubkey_ed25519": hex.EncodeToString(vicPub),
		"miner_address":        signerAddr(vicPub),
	}, false)
	// PoH claim on a fresh id (no prior poison) should also be fine.
	if code != http.StatusOK && code != http.StatusTooManyRequests {
		// TooManyRequests (no_work / rate) is acceptable for PoH without seeded ranges;
		// payout lock must not be the reject reason.
		reason, _ := out["reason"].(string)
		if strings.Contains(reason, "payout_address_locked") {
			t.Fatalf("PoH claim must not hit poisoned lock: %d %v", code, out)
		}
	}
}

// Anti-snipe still holds: once locked, a foreign key on release is rejected.
func TestReport27ReleaseRejectsLockMismatch(t *testing.T) {
	_, wm, mux := report27Setup(t)
	ownerPub, _, _ := ed25519.GenerateKey(nil)
	atkPub, _, _ := ed25519.GenerateKey(nil)
	wm.notePayoutLock("locked-rig", signerAddr(ownerPub))

	code, out := report27Post(t, mux, "/api/fuzz/work/release", map[string]any{
		"worker_id": "locked-rig", "campaign_id": "r27-camp", "item_id": 1,
		"miner_pubkey": hex.EncodeToString(atkPub), "miner_address": signerAddr(atkPub),
	}, false)
	reason, _ := out["reason"].(string)
	if code != http.StatusForbidden || !strings.HasPrefix(reason, "payout_address_locked") {
		t.Fatalf("want 403 payout_address_locked, got %d %v", code, out)
	}
}

// Variant B: first-come claim squatting remains by design; admin unbind recovers.
func TestReport27AdminUnbindClearsClaimSquat(t *testing.T) {
	db, wm, mux := report27Setup(t)
	atkPub, _, _ := ed25519.GenerateKey(nil)
	vicPub, _, _ := ed25519.GenerateKey(nil)

	code, out := report27Post(t, mux, "/api/fuzz/work/claim", map[string]any{
		"worker_id":      "victim-rig-02",
		"miner_pubkey":   hex.EncodeToString(atkPub),
		"miner_address":  signerAddr(atkPub),
		"worker_version": "9.9.9", "hunt_harness_exec": "libfuzzer_oneshot",
		"hunt_engines": "libfuzzer,aflpp,byte",
	}, false)
	if code != http.StatusOK {
		t.Fatalf("attacker first claim: %d %v", code, out)
	}

	code, out = report27Post(t, mux, "/api/fuzz/work/claim", map[string]any{
		"worker_id":      "victim-rig-02",
		"miner_pubkey":   hex.EncodeToString(vicPub),
		"miner_address":  signerAddr(vicPub),
		"worker_version": "9.9.9", "hunt_harness_exec": "libfuzzer_oneshot",
		"hunt_engines": "libfuzzer,aflpp,byte",
	}, false)
	reason, _ := out["reason"].(string)
	if code != http.StatusForbidden || !strings.Contains(reason, "locked="+signerAddr(atkPub)) {
		t.Fatalf("victim must be locked out before unbind: %d %v", code, out)
	}

	code, out = report27Post(t, mux, "/api/work/admin/unbind-payout-lock", map[string]any{
		"worker_id": "victim-rig-02",
	}, true)
	if code != http.StatusOK || out["cleared"] != true {
		t.Fatalf("admin unbind: %d %v", code, out)
	}
	if got := wm.lockedPayoutAddress("victim-rig-02"); got != "" {
		t.Fatalf("in-memory lock still set: %q", got)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM worker_payout_lock WHERE worker_id=?`, "victim-rig-02").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("durable lock row still present")
	}

	code, out = report27Post(t, mux, "/api/fuzz/work/claim", map[string]any{
		"worker_id":      "victim-rig-02",
		"miner_pubkey":   hex.EncodeToString(vicPub),
		"miner_address":  signerAddr(vicPub),
		"worker_version": "9.9.9", "hunt_harness_exec": "libfuzzer_oneshot",
		"hunt_engines": "libfuzzer,aflpp,byte",
	}, false)
	if code != http.StatusOK {
		t.Fatalf("victim claim after unbind must succeed: %d %v", code, out)
	}
}

func TestReport27UnbindRequiresAdmin(t *testing.T) {
	_, _, mux := report27Setup(t)
	code, _ := report27Post(t, mux, "/api/work/admin/unbind-payout-lock", map[string]any{
		"worker_id": "any-rig",
	}, false)
	if code != http.StatusUnauthorized {
		t.Fatalf("worker token must not unbind, got %d", code)
	}
}

func TestReport27DesktopIdDerivation(t *testing.T) {
	id := workerid.DefaultDesktop()
	t.Logf("DefaultDesktop() = %q", id)
	if !strings.HasPrefix(id, "worker-") {
		t.Fatalf("default desktop id must be worker-<hostname>, got %q", id)
	}
}
