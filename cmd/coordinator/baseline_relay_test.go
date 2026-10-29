package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBaselineRelayDisabledByDefault(t *testing.T) {
	// A9
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "")
	wm := &workManager{ordersProbeURL: "http://127.0.0.1:9"}
	if wm.baselineRelayEnabled() {
		t.Fatal("expected baseline relay off by default")
	}
	res := wm.relayBaselineSolve("HMC-abc", 1, 100)
	if res.OK || res.Reason != "baseline_relay_disabled" {
		t.Fatalf("got %+v", res)
	}
}

func TestBaselineRelayRequiresAdminToken(t *testing.T) {
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "1")
	t.Setenv("HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN", "")
	t.Setenv("HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN", "")
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "")
	wm := &workManager{ordersProbeURL: "http://127.0.0.1:18080"}
	res := wm.relayBaselineSolve("HMC-abc", 1, 100)
	if res.OK || res.Reason != "baseline_admin_token_missing" {
		t.Fatalf("got %+v", res)
	}
}

func TestBaselineRelayPostsSolveBaseline(t *testing.T) {
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "1")
	t.Setenv("HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN", "tok-test")
	var gotPath string
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-Hackme-Admin-Token")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":         true,
			"block_hash": "abc123",
			"baseline":   true,
		})
	}))
	defer srv.Close()
	wm := &workManager{ordersProbeURL: srv.URL}
	res := wm.relayBaselineSolve("HMC-mineraddr0001", 42, 5_000_000)
	if !res.OK || res.BlockHash != "abc123" {
		t.Fatalf("got %+v", res)
	}
	if gotPath != "/api/poh/solve-baseline" {
		t.Fatalf("path=%q", gotPath)
	}
	if gotAuth != "tok-test" {
		t.Fatalf("auth=%q", gotAuth)
	}
	if gotBody["miner_address"] != "HMC-mineraddr0001" {
		t.Fatalf("body=%v", gotBody)
	}
}

func TestBaselineRelayHTTPError(t *testing.T) {
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "1")
	t.Setenv("HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN", "tok")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "chain: invalid poh solution for submitted target_mod"})
	}))
	defer srv.Close()
	wm := &workManager{ordersProbeURL: srv.URL}
	res := wm.relayBaselineSolve("HMC-mineraddr0001", 1, 100)
	if res.OK {
		t.Fatal("expected failure")
	}
	if res.Reason != "chain: invalid poh solution for submitted target_mod" {
		t.Fatalf("reason=%q", res.Reason)
	}
}

func TestBaselineRelayFlagTruthy(t *testing.T) {
	wm := &workManager{}
	for _, v := range []string{"1", "true", "YES", "on"} {
		t.Setenv("HACKME_POOL_BASELINE_RELAY", v)
		if !wm.baselineRelayEnabled() {
			t.Fatalf("%q should enable", v)
		}
	}
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "0")
	if wm.baselineRelayEnabled() {
		t.Fatal("0 should disable")
	}
}

func TestBaselineRelayCounters(t *testing.T) {
	wm := &workManager{}
	wm.baselineRelayOK++
	wm.baselineRelayReject += 2
	if wm.baselineRelayOK != 1 || wm.baselineRelayReject != 2 {
		t.Fatalf("ok=%d rej=%d", wm.baselineRelayOK, wm.baselineRelayReject)
	}
}
