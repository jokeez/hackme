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
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "0")
	t.Setenv("HACKME_POOL_BASELINE_RELAY_ALLOW_LOCAL_POH", "")
	for _, v := range []string{"1", "true", "YES", "on"} {
		t.Setenv("HACKME_POOL_BASELINE_RELAY", v)
		if !wm.baselineRelayEnabled() || wm.baselineRelayMode() != "on" {
			t.Fatalf("%q should enable live mode", v)
		}
	}
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "0")
	if wm.baselineRelayEnabled() || wm.baselineRelayMode() != "off" {
		t.Fatal("0 should disable")
	}
}

func TestBaselineRelayOnBlockedWhenLocalPoH(t *testing.T) {
	wm := &workManager{}
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "1")
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "1")
	t.Setenv("HACKME_POOL_BASELINE_RELAY_ALLOW_LOCAL_POH", "")
	if wm.baselineRelayMode() != "blocked" || wm.baselineRelayEnabled() {
		t.Fatalf("mode=%q enabled=%v want blocked/disabled", wm.baselineRelayMode(), wm.baselineRelayEnabled())
	}
	res := wm.relayBaselineSolve("HMC-mineraddr0001", 1, 100)
	if res.OK || res.Reason != "baseline_relay_blocked_local_poh" {
		t.Fatalf("got %+v", res)
	}
	// Escape hatch for lab.
	t.Setenv("HACKME_POOL_BASELINE_RELAY_ALLOW_LOCAL_POH", "1")
	if wm.baselineRelayMode() != "on" || !wm.baselineRelayEnabled() {
		t.Fatalf("allow escape: mode=%q", wm.baselineRelayMode())
	}
	// Dry still works with local PoH (observe-only).
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "dry")
	t.Setenv("HACKME_POOL_BASELINE_RELAY_ALLOW_LOCAL_POH", "")
	if wm.baselineRelayMode() != "dry" || !wm.baselineRelayEnabled() {
		t.Fatalf("dry should stay enabled under local PoH, mode=%q", wm.baselineRelayMode())
	}
}

func TestApplyChainTargetModPinsWhenBaselineRelayOn(t *testing.T) {
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "on")
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "0")
	t.Setenv("HACKME_POOL_BASELINE_RELAY_ALLOW_LOCAL_POH", "")
	wm := &workManager{
		targetMod:         90_000_000_000,
		targetModMin:      2_000_000,
		targetModMax:      1_000_000_000_000,
		poolRetarget:      true,
		pohFormulaVersion: 3,
	}
	wm.applyChainTargetMod(27_428_398, 1_700_000_000)
	if wm.targetMod != 27_428_398 {
		t.Fatalf("RELAY=on must pin pool M to chain M, got %d", wm.targetMod)
	}
}

func TestActiveOrderProbeStatusFailClosed(t *testing.T) {
	wm := &workManager{
		ordersProbeURL:      "http://127.0.0.1:9",
		ordersProbeEverySec: 0,
	}
	snap, ok := wm.activeOrderSnapshotStatus()
	if ok {
		t.Fatal("dead probe URL must report probeOK=false")
	}
	if snap.ID != "" {
		t.Fatalf("unexpected snap id=%q", snap.ID)
	}
	// Successful empty catalog is probeOK=true (baseline allowed).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{}})
	}))
	defer srv.Close()
	wm2 := &workManager{ordersProbeURL: srv.URL, ordersProbeEverySec: 0}
	snap, ok = wm2.activeOrderSnapshotStatus()
	if !ok || snap.ID != "" {
		t.Fatalf("empty catalog: ok=%v snap=%+v", ok, snap)
	}
}

func TestBaselineRelayDryModeNoHTTP(t *testing.T) {
	t.Setenv("HACKME_POOL_BASELINE_RELAY", "dry")
	hit := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit++
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	wm := &workManager{ordersProbeURL: srv.URL}
	if wm.baselineRelayMode() != "dry" || !wm.baselineRelayEnabled() {
		t.Fatalf("mode=%q enabled=%v", wm.baselineRelayMode(), wm.baselineRelayEnabled())
	}
	res := wm.relayBaselineSolve("HMC-mineraddr0001", 7, 1000)
	if !res.OK || res.Reason != "baseline_relay_dry" {
		t.Fatalf("got %+v", res)
	}
	if hit != 0 {
		t.Fatalf("dry mode must not POST, hit=%d", hit)
	}
	for _, v := range []string{"shadow", "observe"} {
		t.Setenv("HACKME_POOL_BASELINE_RELAY", v)
		if wm.baselineRelayMode() != "dry" {
			t.Fatalf("%q want dry mode", v)
		}
	}
}

func TestBaselineRelayCounters(t *testing.T) {
	wm := &workManager{}
	wm.baselineRelayOK++
	wm.baselineRelayReject += 2
	wm.baselineRelayWould++
	if wm.baselineRelayOK != 1 || wm.baselineRelayReject != 2 || wm.baselineRelayWould != 1 {
		t.Fatalf("ok=%d rej=%d would=%d", wm.baselineRelayOK, wm.baselineRelayReject, wm.baselineRelayWould)
	}
}
