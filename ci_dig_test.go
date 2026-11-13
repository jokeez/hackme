package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hackme/internal/chain"
	"hackme/internal/fuzzingcli"
	"hackme/internal/sandbox"
)

const (
	testCIToken    = "ci-token-0123456789abcdef0123456789abcdef"
	testCIAdminTok = "ci-test-admin-token"
)

func newCIDigTestApp(t *testing.T) (*app, *sql.DB) {
	t.Helper()
	a, db := newWalletTestApp(t)
	addr, _, _ := a.chain.Wallet(context.Background())
	units := chain.HMCToUnits(200)
	if _, err := db.ExecContext(context.Background(), `UPDATE wallet SET balance_hmc=200, balance_units=? WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_ADMIN_TOKEN", testCIAdminTok)
	t.Setenv("HACKME_DEVELOPER_TOKEN", "")
	t.Setenv("HACKME_CI_TOKEN", testCIToken)
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	for _, k := range []string{"HACKME_CI_MAX_PER_HOUR", "HACKME_CI_MAX_PER_DAY", "HACKME_CI_MAX_HMC_PER_DAY", "HACKME_CI_ALLOW_DEEP", "HACKME_CI_ALLOW_POOL"} {
		t.Setenv(k, "")
	}
	return a, db
}

func ciDig(t *testing.T, a *app, header, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf []byte
	switch b := body.(type) {
	case string:
		buf = []byte(b)
	default:
		buf, _ = json.Marshal(b)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/ci/dig", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set(header, token)
	}
	// Fresh per-IP window: these tests are about auth and caps, not the 2/s limiter.
	a.rlMu.Lock()
	a.rlHits = make(map[string]rateSlot)
	a.rlBan = make(map[string]int64)
	a.rlMu.Unlock()
	rec := httptest.NewRecorder()
	a.handleCIDig(rec, req)
	return rec
}

func okBody() map[string]any {
	return map[string]any{"wasm_check_hex": sandbox.MinimalGateWasmHex, "dig_package": "scan", "label": "acme/lib#7"}
}

func TestCIDigCreatesScopedOrder(t *testing.T) {
	a, db := newCIDigTestApp(t)
	rec := ciDig(t, a, "X-Hackme-CI-Token", testCIToken, okBody())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	tok, _ := resp["customer_report_token"].(string)
	camp, _ := resp["campaign_id"].(string)
	if tok == "" || camp == "" {
		t.Fatalf("want campaign + report token for the gate step: %v", resp)
	}
	var owner, cfg string
	if err := db.QueryRowContext(context.Background(), `SELECT owner_ref, config_json FROM fuzz_campaigns WHERE id=?`, camp).Scan(&owner, &cfg); err != nil {
		t.Fatal(err)
	}
	if owner != "ci:acme/lib#7" {
		t.Fatalf("owner_ref=%q", owner)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(cfg), &m)
	if m["dig_package"] != "scan" || m["public_proof"] == true {
		t.Fatalf("config: %v", m)
	}
}

func TestCIDigAuth(t *testing.T) {
	a, _ := newCIDigTestApp(t)
	cases := []struct {
		name, header, token string
	}{
		{"no token", "", ""},
		{"wrong token", "X-Hackme-CI-Token", testCIToken + "x"},
		{"admin token on ci header", "X-Hackme-CI-Token", testCIAdminTok},
		{"admin header", "X-Hackme-Admin-Token", testCIAdminTok},
		{"ci token as bearer", "Authorization", "Bearer " + testCIToken},
	}
	for _, c := range cases {
		if rec := ciDig(t, a, c.header, c.token, okBody()); rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", c.name, rec.Code)
		}
	}

	// The CI token must not open the admin route either.
	body, _ := json.Marshal(okBody())
	for _, h := range []string{"X-Hackme-Admin-Token", "X-Hackme-CI-Token"} {
		req := httptest.NewRequest(http.MethodPost, "/api/security-audit", bytes.NewReader(body))
		req.Header.Set(h, testCIToken)
		a.rlHits = make(map[string]rateSlot)
		rec := httptest.NewRecorder()
		a.handleSecurityAudit(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("security-audit with CI token via %s: status %d", h, rec.Code)
		}
	}
}

func TestCIDigTokenConfig(t *testing.T) {
	a, _ := newCIDigTestApp(t)
	t.Setenv("HACKME_CI_TOKEN", "")
	if rec := ciDig(t, a, "X-Hackme-CI-Token", "", okBody()); rec.Code != http.StatusNotFound {
		t.Fatalf("unset token: status %d, want 404 (route off)", rec.Code)
	}
	for name, tok := range map[string]string{
		"too short":      "short-ci-token",
		"same as admin":  testCIAdminTok + strings.Repeat("x", 0),
		"same as dev":    strings.Repeat("d", 40),
		"admin, padded ": testCIAdminTok,
	} {
		t.Setenv("HACKME_DEVELOPER_TOKEN", strings.Repeat("d", 40))
		t.Setenv("HACKME_CI_TOKEN", tok)
		if rec := ciDig(t, a, "X-Hackme-CI-Token", tok, okBody()); rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503", name, rec.Code)
		}
	}
}

func TestCIDigRejectsOutOfScopeRequests(t *testing.T) {
	a, _ := newCIDigTestApp(t)
	hex := sandbox.MinimalGateWasmHex
	cases := map[string]any{
		"client budget":       map[string]any{"wasm_check_hex": hex, "budget_hmc": 400},
		"public proof":        map[string]any{"wasm_check_hex": hex, "public_proof": true},
		"wasm_path":           map[string]any{"wasm_path": "tasks/artifacts/x.wasm"},
		"code compile":        map[string]any{"code": "fn check", "language": "rust"},
		"payer wallet":        map[string]any{"wasm_check_hex": hex, "payer_wallet": "HMC-1"},
		"hunt package":        map[string]any{"wasm_check_hex": hex, "dig_package": "hunt_heavy"},
		"deep without opt-in": map[string]any{"wasm_check_hex": hex, "dig_package": "deep"},
		"preset + hex":        map[string]any{"wasm_check_hex": hex, "dig_preset": "scan_smoke"},
		"nothing":             map[string]any{"dig_package": "scan"},
		"bad label":           map[string]any{"wasm_check_hex": hex, "label": "a%b"},
		"internal gate title": map[string]any{"wasm_check_hex": hex, "title": "Pool Sync gate"},
		"trailing json":       `{"wasm_check_hex":"` + hex + `"} {"budget_hmc":400}`,
	}
	for name, body := range cases {
		rec := ciDig(t, a, "X-Hackme-CI-Token", testCIToken, body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (%s)", name, rec.Code, strings.TrimSpace(rec.Body.String()))
		}
	}
}

func TestCIDigCaps(t *testing.T) {
	a, _ := newCIDigTestApp(t)
	t.Setenv("HACKME_CI_MAX_PER_HOUR", "2")
	for i := 0; i < 2; i++ {
		if rec := ciDig(t, a, "X-Hackme-CI-Token", testCIToken, okBody()); rec.Code != http.StatusOK {
			t.Fatalf("order %d: status %d %s", i, rec.Code, rec.Body.String())
		}
	}
	rec := ciDig(t, a, "X-Hackme-CI-Token", testCIToken, okBody())
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "ci_cap_exceeded") {
		t.Fatalf("3rd order: status %d %s", rec.Code, rec.Body.String())
	}

	// HMC cap: two scan orders already spent their budget today.
	t.Setenv("HACKME_CI_MAX_PER_HOUR", "100")
	t.Setenv("HACKME_CI_MAX_HMC_PER_DAY", "2.5")
	rec = ciDig(t, a, "X-Hackme-CI-Token", testCIToken, okBody())
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "HMC") {
		t.Fatalf("HMC cap: status %d %s", rec.Code, rec.Body.String())
	}

	t.Setenv("HACKME_CI_MAX_HMC_PER_DAY", "100")
	t.Setenv("HACKME_CI_MAX_PER_DAY", "2")
	rec = ciDig(t, a, "X-Hackme-CI-Token", testCIToken, okBody())
	if rec.Code != http.StatusTooManyRequests || !strings.Contains(rec.Body.String(), "daily order") {
		t.Fatalf("day cap: status %d %s", rec.Code, rec.Body.String())
	}
}

func TestCIBuildAuditRequestServerPicksBudget(t *testing.T) {
	req, err := ciBuildAuditRequest(ciDigRequest{DigPreset: "smoke"}, ciPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if req.DigPreset != "scan_smoke" || req.GuardPack != "overflow_smoke" || req.DigPackage != "scan" {
		t.Fatalf("preset resolution: %+v", req)
	}
	if req.BudgetHMC <= 0 || req.BudgetRuns <= 0 || req.PublicProof == nil || *req.PublicProof {
		t.Fatalf("server-side budget/flags: %+v", req)
	}
	if req.PayerRef != "ci:ci" || req.UseSUPDiscount != nil || req.PayerWallet != "" || req.WasmPath != "" || req.Code != "" {
		t.Fatalf("fields CI must never set: %+v", req)
	}
	if _, err := ciBuildAuditRequest(ciDigRequest{DigPreset: "secrets", DigPackage: "deep"}, ciPolicy{AllowDeep: true}); err != nil {
		t.Fatalf("deep with opt-in: %v", err)
	}
}

// audit/deep packages default to pool + PoH; the CI path must stay local
// unless the operator opts in with HACKME_CI_ALLOW_POOL (#89 review).
func TestCIBuildAuditRequestLocalByDefault(t *testing.T) {
	for _, pkg := range []string{"scan", "audit", "deep"} {
		req, err := ciBuildAuditRequest(ciDigRequest{DigPreset: "overflow_smoke", DigPackage: pkg}, ciPolicy{AllowDeep: true})
		if err != nil {
			t.Fatal(err)
		}
		if req.PoolDistributed == nil || *req.PoolDistributed || req.CreatePoHOrder == nil || *req.CreatePoHOrder {
			t.Errorf("%s: CI must default to local Dig without PoH, got pool=%v poh=%v", pkg, *req.PoolDistributed, *req.CreatePoHOrder)
		}
	}
	req, err := ciBuildAuditRequest(ciDigRequest{DigPreset: "overflow_smoke", DigPackage: "audit"}, ciPolicy{AllowPool: true})
	if err != nil {
		t.Fatal(err)
	}
	if !*req.PoolDistributed || !*req.CreatePoHOrder {
		t.Errorf("with HACKME_CI_ALLOW_POOL audit keeps its package defaults, got pool=%v poh=%v", *req.PoolDistributed, *req.CreatePoHOrder)
	}
}

// End to end: an audit order through the handler lands as a local campaign.
func TestCIDigAuditStaysLocal(t *testing.T) {
	a, db := newCIDigTestApp(t)
	body := okBody()
	body["dig_package"] = "audit"
	rec := ciDig(t, a, "X-Hackme-CI-Token", testCIToken, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	var cfg string
	if err := db.QueryRowContext(context.Background(), `SELECT config_json FROM fuzz_campaigns WHERE id=?`, resp["campaign_id"]).Scan(&cfg); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(cfg), &m)
	if m["pool_distributed"] != false || m["dig_package"] != "audit" || m["attach_poh_order"] == true {
		t.Fatalf("audit via CI should be a local Dig: %v", m)
	}
	if resp["order"] != nil {
		t.Fatalf("no PoH order expected for CI audit: %v", resp["order"])
	}
}

// Own wasm sent by the action (often with guard_pack for mutator defaults) must
// be a customer_guard, so its violations fail the gate instead of becoming
// coverage noise and a false PASS in `mode: gate` (main 8647a3c9).
func TestCIDigOwnWasmIsCustomerGuard(t *testing.T) {
	for name, body := range map[string]map[string]any{
		"own wasm":              okBody(),
		"own wasm + guard_pack": func() map[string]any { b := okBody(); b["guard_pack"] = "overflow_smoke"; return b }(),
	} {
		t.Run(name, func(t *testing.T) {
			a, db := newCIDigTestApp(t)
			rec := ciDig(t, a, "X-Hackme-CI-Token", testCIToken, body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
			}
			if m := ciCampaignConfig(t, db, rec); m["customer_guard"] != true {
				t.Fatalf("own wasm must be customer_guard: %v", m)
			}
		})
	}
}

// A shipped preset stays a detector pack (no customer_guard).
func TestCIDigPresetIsNotCustomerGuard(t *testing.T) {
	pack, _ := fuzzingcli.GuardPackFor("overflow_smoke")
	if _, _, err := fuzzingcli.LoadPackWasmHex(fuzzingcli.DiscoverRepoRoot(), pack); err != nil {
		t.Skip("overflow_smoke wasm not built here (scripts/ops/build_scan_smoke_guards.sh):", err)
	}
	a, db := newCIDigTestApp(t)
	rec := ciDig(t, a, "X-Hackme-CI-Token", testCIToken, map[string]any{"dig_preset": "overflow_smoke", "dig_package": "scan"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	if m := ciCampaignConfig(t, db, rec); m["customer_guard"] == true {
		t.Fatalf("shipped preset must not be customer_guard: %v", m)
	}
}

func ciCampaignConfig(t *testing.T, db *sql.DB, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var cfg string
	if err := db.QueryRowContext(context.Background(), `SELECT config_json FROM fuzz_campaigns WHERE id=?`, resp["campaign_id"]).Scan(&cfg); err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(cfg), &m); err != nil {
		t.Fatal(err)
	}
	return m
}
