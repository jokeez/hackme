package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/sandbox"
)

func TestSecurityAuditWasmHex(t *testing.T) {
	a, db := newWalletTestApp(t)
	addr, _, _ := a.chain.Wallet(context.Background())
	units := chain.HMCToUnits(50)
	if _, err := db.ExecContext(context.Background(), `UPDATE wallet SET balance_hmc=50, balance_units=? WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_ADMIN_TOKEN", "audit-admin-test")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")

	body, _ := json.Marshal(map[string]any{
		"title":            "audit-test",
		"budget_hmc":       0.5,
		"budget_runs":      8,
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
		"create_poh_order": true,
		"pool_distributed": false, // local PoH path; no coordinator in unit test
	})
	req := httptest.NewRequest(http.MethodPost, "/api/security-audit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "audit-admin-test")
	rec := httptest.NewRecorder()
	a.rlHits = make(map[string]rateSlot)
	a.rlBan = make(map[string]int64)
	a.handleSecurityAudit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["ok"] != true {
		t.Fatalf("ok false: %v", resp)
	}
	if resp["customer_report_token"] == nil || resp["customer_report_token"] == "" {
		t.Fatal("missing report token")
	}
}

func TestSecurityAuditDigPresetOneClick(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	wasmPath := filepath.Join(root, "tasks", "artifacts", "security", "rust_overflow_smoke_guard.wasm")
	if _, err := os.Stat(wasmPath); err != nil {
		t.Skip("overflow_smoke wasm not shipped:", err)
	}

	a, db := newWalletTestApp(t)
	addr, _, _ := a.chain.Wallet(context.Background())
	units := chain.HMCToUnits(50)
	if _, err := db.ExecContext(context.Background(), `UPDATE wallet SET balance_hmc=50, balance_units=? WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_ADMIN_TOKEN", "audit-admin-test")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	t.Setenv("HACKME_REPO_ROOT", root)
	t.Setenv("HACKME_FROM_CODE", "0")

	body, _ := json.Marshal(map[string]any{
		"title":            "preset-one-click",
		"campaign_id":      "campaign-dig-preset-smoke",
		"order_id":         "order-dig-preset-smoke",
		"dig_preset":       "scan_smoke",
		"create_poh_order": false,
		"pool_distributed": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/security-audit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "audit-admin-test")
	rec := httptest.NewRecorder()
	a.rlHits = make(map[string]rateSlot)
	a.rlBan = make(map[string]int64)
	a.handleSecurityAudit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["ok"] != true {
		t.Fatalf("ok false: %v", resp)
	}
	if resp["dig_preset"] != "scan_smoke" {
		t.Fatalf("dig_preset=%v", resp["dig_preset"])
	}
	if resp["guard_pack"] != "overflow_smoke" {
		t.Fatalf("guard_pack=%v", resp["guard_pack"])
	}
	if resp["customer_report_token"] == nil || resp["customer_report_token"] == "" {
		t.Fatal("missing report token")
	}
	camp, _ := resp["campaign"].(map[string]any)
	if camp == nil {
		t.Fatalf("missing campaign: %v", resp)
	}
	cfg, _ := camp["config"].(map[string]any)
	if cfg == nil {
		t.Fatalf("missing campaign.config: %v", camp)
	}
	if cfg["guard_pack"] != "overflow_smoke" {
		t.Fatalf("config guard_pack=%v", cfg["guard_pack"])
	}
	if strings.TrimSpace(toString(cfg["wasm_check_hex"])) == "" {
		t.Fatal("expected wasm_check_hex resolved from preset")
	}
}

func TestSecurityAuditGuardPackWithoutHex(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	wasmPath := filepath.Join(root, "tasks", "artifacts", "security", "rust_script_push_bounds_guard.wasm")
	if _, err := os.Stat(wasmPath); err != nil {
		t.Skip("script_bounds wasm not shipped:", err)
	}

	a, db := newWalletTestApp(t)
	addr, _, _ := a.chain.Wallet(context.Background())
	units := chain.HMCToUnits(50)
	if _, err := db.ExecContext(context.Background(), `UPDATE wallet SET balance_hmc=50, balance_units=? WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_ADMIN_TOKEN", "audit-admin-test")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	t.Setenv("HACKME_REPO_ROOT", root)
	t.Setenv("HACKME_FROM_CODE", "0")

	body, _ := json.Marshal(map[string]any{
		"guard_pack":       "script_bounds",
		"dig_package":      "scan",
		"budget_hmc":       1.0,
		"budget_runs":      16,
		"create_poh_order": false,
		"pool_distributed": false,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/security-audit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "audit-admin-test")
	rec := httptest.NewRecorder()
	a.rlHits = make(map[string]rateSlot)
	a.rlBan = make(map[string]int64)
	a.handleSecurityAudit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["guard_pack"] != "script_bounds" {
		t.Fatalf("guard_pack=%v", resp["guard_pack"])
	}
}

func TestSecurityAuditPoolDistributedAttachesPoH(t *testing.T) {
	a, db := newWalletTestApp(t)
	addr, _, _ := a.chain.Wallet(context.Background())
	units := chain.HMCToUnits(50)
	if _, err := db.ExecContext(context.Background(), `UPDATE wallet SET balance_hmc=50, balance_units=? WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_ADMIN_TOKEN", "audit-admin-test")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "http://127.0.0.1:9")
	t.Setenv("HACKME_COORDINATOR_ADMIN_TOKEN", "coord-admin")

	body, _ := json.Marshal(map[string]any{
		"title":            "audit-pool-attach",
		"order_id":         "order-pool-attach-test",
		"campaign_id":      "campaign-pool-attach-test",
		"budget_hmc":       0.5,
		"budget_runs":      8,
		"wasm_check_hex":   sandbox.MinimalGateWasmHex,
		"create_poh_order": true,
		"pool_distributed": true,
		"reward_hmc":       0.05,
		"target_solves":    2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/security-audit", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "audit-admin-test")
	rec := httptest.NewRecorder()
	a.rlHits = make(map[string]rateSlot)
	a.rlBan = make(map[string]int64)
	a.handleSecurityAudit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	order, _ := resp["order"].(map[string]any)
	if order == nil || order["attach_via_pool"] != true {
		t.Fatalf("expected attach_via_pool order: %v", resp["order"])
	}
	if order["status"] != "attach_pending" {
		t.Fatalf("status=%v", order["status"])
	}
	// Must NOT open a local PoH task — prepaid stays for pool ORDERS attach.
	rows, err := a.chain.ListOrderTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.ID == "order-pool-attach-test" {
			t.Fatalf("local PoH order should not exist for pool_distributed attach")
		}
	}
}
