package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hackme/internal/sandbox"
)

func TestConfigTruthyAttachFlags(t *testing.T) {
	cfg := map[string]any{"attach_poh_order": true, "create_poh_order": "1"}
	if !configTruthy(cfg, "attach_poh_order") {
		t.Fatal("attach_poh_order true")
	}
	if !configTruthy(cfg, "create_poh_order") {
		t.Fatal("create_poh_order string 1")
	}
	if configTruthy(map[string]any{"create_poh_order": false}, "attach_poh_order", "create_poh_order") {
		t.Fatal("should be false")
	}
}

func TestAttachPoHOrderFromFuzzConfig(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tasks" || r.Method != http.MethodPost {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		if r.Header.Get("X-Hackme-Admin-Token") != "tok" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "id": gotBody["id"], "prepaid_hmc": 0.05, "total_debit_hmc": 0.0525,
		})
	}))
	defer srv.Close()

	t.Setenv("HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN", "tok")
	wm := &workManager{ordersProbeURL: srv.URL}
	cfg := map[string]any{
		"attach_poh_order":     true,
		"wasm_check_hex":       sandbox.MinimalGateWasmHex,
		"poh_order_id":         "order-attach-test-1",
		"poh_reward_hmc":       0.05,
		"poh_target_solves":    1,
		"poh_difficulty_score": 10,
		"poh_payer_ref":        "test:attach",
	}
	out := wm.attachPoHOrderFromFuzzConfig("campaign-attach-test-1", cfg)
	if out["ok"] != true {
		t.Fatalf("attach failed: %v", out)
	}
	if gotBody["id"] != "order-attach-test-1" {
		t.Fatalf("posted id=%v", gotBody["id"])
	}
	if gotBody["kind"] != "synthetic_poh_v1" {
		t.Fatalf("kind=%v", gotBody["kind"])
	}
	wasm, _ := gotBody["wasm_check_hex"].(string)
	if !strings.EqualFold(wasm, sandbox.MinimalGateWasmHex) {
		t.Fatalf("wasm mismatch")
	}
}

func TestAttachPoHOrderIdempotentExists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": `chain: task id "order-x" already exists`})
	}))
	defer srv.Close()
	t.Setenv("HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN", "tok")
	wm := &workManager{ordersProbeURL: srv.URL}
	out := wm.attachPoHOrderFromFuzzConfig("c1", map[string]any{
		"create_poh_order": true,
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
		"poh_order_id":     "order-x",
	})
	if out["ok"] != true || out["reason"] != "already_exists" {
		t.Fatalf("want already_exists ok: %v", out)
	}
}

func TestAttachPoHOrderSkippedWhenFlagOff(t *testing.T) {
	wm := &workManager{ordersProbeURL: "http://127.0.0.1:9"}
	out := wm.attachPoHOrderFromFuzzConfig("c1", map[string]any{
		"wasm_check_hex": sandbox.MinimalGateWasmHex,
	})
	if out["skipped"] != true || out["reason"] != "flag_off" {
		t.Fatalf("want flag_off skip: %v", out)
	}
}
