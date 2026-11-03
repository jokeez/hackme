package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hackme/internal/chain"
)

// A found whose baseline relay fails (chain reject, tip race, probe outage) must cost only the
// found — never the lease's verified shares. Rejecting the whole submit made hiding founds the
// better strategy for miners.
func TestBaselineRelayFailureKeepsSharePay(t *testing.T) {
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "on")
	t.Setenv("HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN", "tok-test")
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
		case "/api/poh/solve-baseline":
			posts++
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "chain: tip moved"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	addr := signerAddr(pub)
	wm := secWM(t)
	// targetMod=1 guarantees a block hit in any lease (mod=2000 + random key was flaky).
	wm.defaultBatch, wm.maxClaimBatch, wm.targetMod = 1<<12, 1<<12, 1
	wm.ordersProbeURL = srv.URL
	wm.hybridSignerEnabled = true
	wm.acceptedSubmitNonces = make(map[string]struct{})
	wm.acceptedSignedPayloads = make(map[string]struct{})
	wm.signedSubmitNonceMax = make(map[string]uint64)

	l := secClaim(t, wm, "w-relay", addr)
	shares := honestShares(l, l.prev, l.addr, 128)
	if len(shares) == 0 {
		t.Fatal("no shares in lease")
	}
	var found uint64
	ok := false
	for n := l.base; n < l.base+l.size; n++ {
		if chain.PohWorkBlake3(l.prev, l.addr, n)%l.mod == 0 {
			found, ok = n, true
			break
		}
	}
	if !ok {
		t.Fatal("no block hit in lease")
	}
	req := submitWorkRequest{
		WorkerID: "w-relay", BaseNonce: l.base, BatchSize: l.size, WorkID: buildWorkID("w-relay", l.base, l.size),
		Attempts: l.size, HashrateGHS: 1, ShareNonces: shares,
		Found: true, FoundNonce: found, ResultHash: "rh-relay-1",
		SubmitNonce: 1, MinerPubKey: hex.EncodeToString(pub), MinerAddress: addr, MinerSigAlg: "ed25519",
	}
	req.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(req)))
	_, reason, payout, _, _ := wm.submit(req)
	if reason != "" || payout <= 0 {
		t.Fatalf("relay failure must keep share pay: reason=%q payout=%v", reason, payout)
	}
	if posts != 1 || wm.baselineRelayReject != 1 || wm.baselineRelayDowngraded != 1 || wm.baselineRelayOK != 0 {
		t.Fatalf("posts=%d reject=%d downgraded=%d ok=%d", posts, wm.baselineRelayReject, wm.baselineRelayDowngraded, wm.baselineRelayOK)
	}
	if _, still := wm.active[workKey{base: l.base, batch: l.size}]; still {
		t.Fatal("paid lease must be consumed")
	}
	// The verified hit still counts for the found-rate audit.
	if st := wm.foundAudit[foundAuditKey(addr, "w-relay")]; st == nil || st.Actual != 1 {
		t.Fatalf("found audit not credited: %+v", st)
	}
	// Same signed bytes again: replay, never a second payment.
	if _, reason2, payout2, _, _ := wm.submit(req); payout2 != 0 || reason2 == "" {
		t.Fatalf("replay after downgrade paid %v (reason %q)", payout2, reason2)
	}
}

// C1: while baseline relay holds the work mutex unlocked, a concurrent submit on the same
// range must not also accrue — lease is consumed before HTTP so only the relay owner pays.
func TestBaselineRelayConsumesLeaseBeforeUnlock(t *testing.T) {
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "on")
	t.Setenv("HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN", "tok-test")
	entered := make(chan struct{})
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
		case "/api/poh/solve-baseline":
			close(entered)
			<-release
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "hash": "deadbeef"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	addr := signerAddr(pub)
	wm := secWM(t)
	wm.defaultBatch, wm.maxClaimBatch, wm.targetMod = 1<<12, 1<<12, 1
	wm.ordersProbeURL = srv.URL
	wm.hybridSignerEnabled = true
	wm.acceptedSubmitNonces = make(map[string]struct{})
	wm.acceptedSignedPayloads = make(map[string]struct{})
	wm.signedSubmitNonceMax = make(map[string]uint64)

	l := secClaim(t, wm, "w-c1", addr)
	shares := honestShares(l, l.prev, l.addr, 128)
	if len(shares) == 0 {
		t.Fatal("no shares in lease")
	}
	var found uint64
	okFound := false
	for n := l.base; n < l.base+l.size; n++ {
		if chain.PohWorkBlake3(l.prev, l.addr, n)%l.mod == 0 {
			found, okFound = n, true
			break
		}
	}
	if !okFound {
		t.Fatal("no block hit in lease")
	}

	foundReq := submitWorkRequest{
		WorkerID: "w-c1", BaseNonce: l.base, BatchSize: l.size, WorkID: buildWorkID("w-c1", l.base, l.size),
		Attempts: l.size, HashrateGHS: 1, ShareNonces: shares,
		Found: true, FoundNonce: found, ResultHash: "rh-c1-found",
		SubmitNonce: 1, MinerPubKey: hex.EncodeToString(pub), MinerAddress: addr, MinerSigAlg: "ed25519",
	}
	foundReq.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(foundReq)))

	type submitOut struct {
		reason string
		payout float64
	}
	done := make(chan submitOut, 1)
	go func() {
		_, reason, payout, _, _ := wm.submit(foundReq)
		done <- submitOut{reason: reason, payout: payout}
	}()

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("relay did not start")
	}

	// Concurrent share-only submit while relay is in flight — must not pay.
	shareReq := foundReq
	shareReq.Found = false
	shareReq.FoundNonce = 0
	shareReq.ResultHash = ""
	shareReq.SubmitNonce = 2
	shareReq.MinerSig = hex.EncodeToString(ed25519.Sign(priv, canonicalSubmitBytes(shareReq)))
	_, raceReason, racePay, _, _ := wm.submit(shareReq)
	if racePay != 0 || raceReason == "" {
		close(release)
		t.Fatalf("concurrent submit paid during relay: reason=%q payout=%v", raceReason, racePay)
	}

	close(release)
	out := <-done
	if out.reason != "" || out.payout <= 0 {
		t.Fatalf("relay owner must be paid once: reason=%q payout=%v", out.reason, out.payout)
	}
	if wm.baselineRelayOK != 1 {
		t.Fatalf("baseline_relay_ok=%d", wm.baselineRelayOK)
	}
	st := wm.ensureWorkerAccrualLocked("w-c1")
	if st.AcceptedRanges != 1 {
		t.Fatalf("AcceptedRanges=%d want 1 (no double pay)", st.AcceptedRanges)
	}
}
