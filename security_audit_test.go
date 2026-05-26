package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
