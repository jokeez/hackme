package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Mining HTTP policy: local WASM PoH is opt-in (HACKME_CHAIN_LEADER_LOCAL_POH);
// pool participants use POST /api/worker/start. Guard with tests so UI/server stay aligned.

func testMiningPolicyEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-mining-policy-test-32ok!!!!")
	t.Setenv("HACKME_CANONICAL_CHAIN_URL", "")
	t.Setenv("HACKME_P2P_PEERS", "")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	t.Setenv("HACKME_PUBLIC_AUTHORITY_BASE", "")
	t.Setenv("HACKME_DESKTOP_MODE", "")
}

func TestHandleMiningStartDisabledWithoutChainLeaderFlag(t *testing.T) {
	testMiningPolicyEnv(t)
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "")
	a, _ := newWalletTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/mining/start", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "admin-mining-policy-test-32ok!!!!")
	rec := httptest.NewRecorder()
	a.handleMiningStart(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got := out["ok"]; got != false {
		t.Fatalf("ok=%v", got)
	}
	if got, _ := out["code"].(string); got != "local_poh_disabled" {
		t.Fatalf("code=%q", got)
	}
}

func TestHandleMiningStartDisabledInNetworkModeWithoutLeaderFlag(t *testing.T) {
	testMiningPolicyEnv(t)
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "http://127.0.0.1:9")
	a, _ := newWalletTestApp(t)
	req := httptest.NewRequest(http.MethodPost, "/api/mining/start", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "admin-mining-policy-test-32ok!!!!")
	rec := httptest.NewRecorder()
	a.handleMiningStart(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got, _ := out["code"].(string); got != "local_poh_disabled_in_network_mode" {
		t.Fatalf("code=%q", got)
	}
}

func TestHandleMiningStartOKWhenChainLeaderLocalPoHEnabled(t *testing.T) {
	testMiningPolicyEnv(t)
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "1")
	a, _ := newWalletTestApp(t)
	t.Cleanup(func() { a.miner.Stop() })
	req := httptest.NewRequest(http.MethodPost, "/api/mining/start", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "admin-mining-policy-test-32ok!!!!")
	rec := httptest.NewRecorder()
	a.handleMiningStart(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got := out["ok"]; got != true {
		t.Fatalf("ok=%v", got)
	}
	if !a.miner.Running() {
		t.Fatal("expected miner running after mining/start with leader flag")
	}
}
