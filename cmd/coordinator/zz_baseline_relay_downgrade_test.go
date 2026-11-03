package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	wm.defaultBatch, wm.maxClaimBatch, wm.targetMod = 1<<12, 1<<12, 2_000
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
