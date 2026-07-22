package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hackme/internal/chain"
)

func seedFuzzCampaignWithReportToken(t *testing.T, a *app, campaignID, plainToken string) {
	t.Helper()
	sum := sha256.Sum256([]byte(strings.TrimSpace(plainToken)))
	hash := hex.EncodeToString(sum[:])
	now := time.Now().Unix()
	_, err := a.db.ExecContext(context.Background(),
		`INSERT INTO fuzz_campaigns
		 (id, campaign_type, status, title, description, owner_ref, task_id, target_ref, budget_runs, budget_seconds, config_json, summary_json, report_token_hash, report_token_issued_at, created_at, started_at, completed_at)
		 VALUES (?, 'fuzz', 'running', 'auth-test', '', '', '', '', 64, 0, '{}', '{}', ?, ?, ?, 0, 0)`,
		campaignID, hash, now, now)
	if err != nil {
		t.Fatal(err)
	}
}

func TestFuzzReportTokenBypassRejected(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret-auth-test")
	const camp = "auth-bypass-camp"
	const token = "customer-report-token-aaa"
	seedFuzzCampaignWithReportToken(t, a, camp, token)

	paths := []string{
		"/api/fuzz/campaigns/" + camp + "/report?format=json",
		"/api/fuzz/campaigns/" + camp + "/gate",
		"/api/fuzz/campaigns/" + camp + "/pulse",
		"/api/fuzz/campaigns/" + camp + "/escrow",
	}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		a.handleFuzzCampaigns(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s without token: status=%d body=%s", path, rec.Code, rec.Body.String())
		}

		req2 := httptest.NewRequest(http.MethodGet, path, nil)
		req2.Header.Set("X-Hackme-Report-Token", "wrong-token")
		rec2 := httptest.NewRecorder()
		a.handleFuzzCampaigns(rec2, req2)
		if rec2.Code != http.StatusUnauthorized {
			t.Fatalf("%s wrong token: status=%d body=%s", path, rec2.Code, rec2.Body.String())
		}

		req3 := httptest.NewRequest(http.MethodGet, path, nil)
		req3.Header.Set("X-Hackme-Report-Token", "")
		rec3 := httptest.NewRecorder()
		a.handleFuzzCampaigns(rec3, req3)
		if rec3.Code != http.StatusUnauthorized {
			t.Fatalf("%s empty token: status=%d", path, rec3.Code)
		}
	}
}

func TestFuzzReportTokenAllowsEscrowAndGate(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret-auth-test")
	const camp = "auth-ok-camp"
	const token = "customer-report-token-bbb"
	seedFuzzCampaignWithReportToken(t, a, camp, token)

	ctx := context.Background()
	addr, _, err := a.chain.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	units := chain.HMCToUnits(20)
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_hmc=20, balance_units=? WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr); err != nil {
		t.Fatal(err)
	}
	if _, err := a.chain.OpenFuzzEscrow(ctx, camp, 1.0, 64); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"/api/fuzz/campaigns/" + camp + "/escrow",
		"/api/fuzz/campaigns/" + camp + "/gate",
		"/api/fuzz/campaigns/" + camp + "/report?format=json",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-Hackme-Report-Token", token)
		rec := httptest.NewRecorder()
		a.handleFuzzCampaigns(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s with report token: status=%d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestFuzzCrossCampaignReportTokenRejected(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret-auth-test")
	seedFuzzCampaignWithReportToken(t, a, "camp-a", "token-for-a")
	seedFuzzCampaignWithReportToken(t, a, "camp-b", "token-for-b")

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/camp-b/gate", nil)
	req.Header.Set("X-Hackme-Report-Token", "token-for-a")
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("cross-campaign token: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFuzzReportTokenCannotEscalateToAdmin(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "admin-secret-auth-test")
	const camp = "auth-escalation-camp"
	const token = "customer-report-token-ccc"
	seedFuzzCampaignWithReportToken(t, a, camp, token)

	// Report token must not rotate itself via admin-only token endpoint.
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/campaigns/"+camp+"/token", bytes.NewReader([]byte("{}")))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Report-Token", token)
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("report token rotate: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Report token in admin header must not unlock admin settle.
	raw := []byte(`{"kind":"run","campaign_id":"` + camp + `","miner_address":"HMC-1234567890123456","event_id":"abuse:1"}`)
	req2 := httptest.NewRequest(http.MethodPost, "/api/fuzz/pool/settle", bytes.NewReader(raw))
	req2.Header.Set("X-Hackme-Admin-Token", token)
	rec2 := httptest.NewRecorder()
	a.handleFuzzPoolSettle(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("report token as admin settle: status=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// List remains admin-only.
	req3 := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns", nil)
	req3.Header.Set("X-Hackme-Report-Token", token)
	rec3 := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec3, req3)
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("report token list: status=%d", rec3.Code)
	}
}

func TestFuzzGateCleanCampaignPassesByDefault(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	const camp = "gate-clean-pass"
	const token = "gate-token-clean"
	seedFuzzCampaignWithReportToken(t, a, camp, token)
	_, _ = db.ExecContext(context.Background(),
		`UPDATE fuzz_campaigns SET status='completed', summary_json='{"runs_done":64}' WHERE id=?`, camp)

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+camp+"/gate?max_critical=0&max_high=0", nil)
	req.Header.Set("X-Hackme-Report-Token", token)
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gate: %d %s", rec.Code, rec.Body.String())
	}
	var gate map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &gate); err != nil {
		t.Fatal(err)
	}
	if gate["pass"] != true {
		t.Fatalf("clean campaign must PASS with default min_sample_size=0: %+v", gate)
	}
}

func TestFuzzGateCrashFirstIgnoresDetectorCritical(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "")
	const camp = "gate-crash-first"
	const token = "gate-token-ddd"
	seedFuzzCampaignWithReportToken(t, a, camp, token)
	now := time.Now().Unix()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES ('det-1', ?, 'security_violation', 'critical', 'detector', '', '', '', '{}', ?)`,
		camp, now)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.ExecContext(context.Background(),
		`UPDATE fuzz_campaigns SET status='completed', summary_json='{"runs_done":64}' WHERE id=?`, camp)

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+camp+"/gate?max_critical=0&max_high=0", nil)
	req.Header.Set("X-Hackme-Report-Token", token)
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gate: %d %s", rec.Code, rec.Body.String())
	}
	var gate map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &gate); err != nil {
		t.Fatal(err)
	}
	if gate["pass"] != true {
		t.Fatalf("detector critical must not fail crash_first gate: %+v", gate)
	}
	if toString(gate["triage_policy"]) != "crash_first" {
		t.Fatalf("triage_policy=%v", gate["triage_policy"])
	}
	obs, _ := gate["observed"].(map[string]any)
	if intFromAny(obs["critical_count"]) != 0 || intFromAny(obs["crash_count"]) != 0 {
		t.Fatalf("crash-class observed must be 0: %+v", obs)
	}
}

func TestFuzzAdminCanAccessReportEndpoints(t *testing.T) {
	a, _ := newWalletTestApp(t)
	const admin = "admin-secret-auth-test"
	t.Setenv("HACKME_ADMIN_TOKEN", admin)
	seedFuzzCampaignWithReportToken(t, a, "auth-admin-camp", "unused-customer-token")

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/auth-admin-camp/gate", nil)
	req.Header.Set("X-Hackme-Admin-Token", admin)
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin gate: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFuzzMinPerRunReturns402(t *testing.T) {
	a, db := newWalletTestApp(t)
	const admin = "admin-secret-auth-test"
	t.Setenv("HACKME_ADMIN_TOKEN", admin)
	ctx := context.Background()
	addr, _, err := a.chain.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	units := chain.HMCToUnits(50)
	if _, err := db.ExecContext(ctx, `UPDATE wallet SET balance_hmc=50, balance_units=? WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr); err != nil {
		t.Fatal(err)
	}

	// 0.5 HMC / 10000 runs → per-run below MinPerRunUnits → Payment Required.
	body, _ := json.Marshal(map[string]any{
		"id":            "min-per-run-402",
		"campaign_type": "fuzz",
		"title":         "dust-per-run",
		"budget_hmc":    0.5,
		"budget_runs":   10000,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/fuzz/campaigns", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", admin)
	rec := httptest.NewRecorder()
	a.rlHits = make(map[string]rateSlot)
	a.rlBan = make(map[string]int64)
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusPaymentRequired {
		t.Fatalf("MinPerRun underpay open: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("per-run")) && !bytes.Contains(rec.Body.Bytes(), []byte("escrow_failed")) {
		t.Fatalf("expected per-run/escrow error, got %s", rec.Body.String())
	}
	var exists int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_campaigns WHERE id=?`, "min-per-run-402").Scan(&exists)
	if exists != 0 {
		t.Fatal("failed MinPerRun create must roll back campaign row")
	}
}
