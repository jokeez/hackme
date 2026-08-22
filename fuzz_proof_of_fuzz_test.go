package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProofOfFuzzPublicBadgeAndHTML(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "proof-admin-token")
	const camp = "campaign-proof-public-demo"
	const token = "proof-customer-token-xyz"
	seedFuzzCampaignWithReportToken(t, a, camp, token)
	cfg := map[string]any{
		"public_proof": true,
		"guard_pack":   "secrets",
		"depth_tier":   "bytes_corpus",
		"input_mode":   "bytes",
		"wasm_sha256":  strings.Repeat("ab", 32),
	}
	now := time.Now().Unix()
	_, err := db.ExecContext(context.Background(),
		`UPDATE fuzz_campaigns SET status='completed', title=?, budget_runs=256, config_json=?, summary_json=?, completed_at=? WHERE id=?`,
		"HackMe pack · Secrets", marshalMapJSON(cfg), marshalMapJSON(map[string]any{"runs_done": 256}), now, camp)
	if err != nil {
		t.Fatal(err)
	}

	// Public — no token
	req := httptest.NewRequest(http.MethodGet, "/proof/"+camp+"?format=json", nil)
	rec := httptest.NewRecorder()
	a.handleProofPretty(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("proof json %d: %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "public") {
		t.Fatalf("public proof cache want public, got %q", cc)
	}
	var proof map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &proof); err != nil {
		t.Fatal(err)
	}
	if proof["public"] != true {
		t.Fatalf("public=%v", proof["public"])
	}
	gate, _ := proof["gate"].(map[string]any)
	if gate["pass"] != true || gate["label"] != "CLEAN" {
		t.Fatalf("gate=%v", gate)
	}
	facts, _ := proof["facts"].(map[string]any)
	if facts["pack"] != "secrets" {
		t.Fatalf("pack=%v", facts["pack"])
	}
	if strings.Contains(rec.Body.String(), "AKIA") || strings.Contains(rec.Body.String(), "ghp_") {
		t.Fatal("public proof must not leak secret-like finding payloads")
	}

	badgeReq := httptest.NewRequest(http.MethodGet, "/proof/"+camp+"/badge.svg", nil)
	badgeRec := httptest.NewRecorder()
	a.handleProofPretty(badgeRec, badgeReq)
	if badgeRec.Code != http.StatusOK {
		t.Fatalf("badge %d", badgeRec.Code)
	}
	ct := badgeRec.Header().Get("Content-Type")
	if !strings.Contains(ct, "svg") {
		t.Fatalf("content-type=%q", ct)
	}
	body := badgeRec.Body.String()
	if !strings.Contains(body, "PROOF OF FUZZ") || !strings.Contains(body, "CLEAN") {
		t.Fatalf("badge body=%s", body)
	}

	htmlReq := httptest.NewRequest(http.MethodGet, "/proof/"+camp, nil)
	htmlReq.Header.Set("Accept", "text/html")
	htmlRec := httptest.NewRecorder()
	a.handleProofPretty(htmlRec, htmlReq)
	if htmlRec.Code != http.StatusOK {
		t.Fatalf("html %d", htmlRec.Code)
	}
	if !strings.Contains(htmlRec.Body.String(), "pass ≠ proven secure") {
		t.Fatal("missing honesty note")
	}
}

func TestProofOfFuzzPrivateCacheNoStore(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "proof-admin-token")
	const camp = "campaign-proof-nostore"
	const token = "proof-nostore-token"
	seedFuzzCampaignWithReportToken(t, a, camp, token)

	req := httptest.NewRequest(http.MethodGet, "/proof/"+camp+"?format=html", nil)
	req.Header.Set("X-Hackme-Report-Token", token)
	rec := httptest.NewRecorder()
	a.handleProofPretty(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("private proof %d: %s", rec.Code, rec.Body.String())
	}
	cc := rec.Header().Get("Cache-Control")
	if !strings.Contains(cc, "no-store") {
		t.Fatalf("private proof must be no-store, got %q", cc)
	}
	if strings.Contains(cc, "public") {
		t.Fatalf("private proof must not be public cacheable: %q", cc)
	}

	badge := httptest.NewRequest(http.MethodGet, "/proof/"+camp+"/badge.svg", nil)
	badge.Header.Set("X-Hackme-Report-Token", token)
	badgeRec := httptest.NewRecorder()
	a.handleProofPretty(badgeRec, badge)
	if badgeRec.Code != http.StatusOK {
		t.Fatalf("private badge %d", badgeRec.Code)
	}
	bcc := badgeRec.Header().Get("Cache-Control")
	if !strings.Contains(bcc, "no-store") || strings.Contains(bcc, "public") {
		t.Fatalf("private badge cache=%q", bcc)
	}
}

func TestProofOfFuzzHTMLEscapesTitle(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "proof-xss-admin")
	const camp = "campaign-proof-xss"
	const token = "proof-xss-token"
	seedFuzzCampaignWithReportToken(t, a, camp, token)
	cfg := map[string]any{"public_proof": true}
	now := time.Now().Unix()
	_, err := db.ExecContext(context.Background(),
		`UPDATE fuzz_campaigns SET status='completed', title=?, config_json=?, summary_json=?, completed_at=? WHERE id=?`,
		`<script>alert(1)</script>AKIAIOSFODNN7EXAMPLE`, marshalMapJSON(cfg),
		marshalMapJSON(map[string]any{"runs_done": 8}), now, camp)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/proof/"+camp, nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	a.handleProofPretty(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Fatal("unescaped script in proof HTML")
	}
	if strings.Contains(body, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatal("full AWS-like title must be redacted on public proof")
	}
}

func TestProofOfFuzzOmitsFindingsAndNeedsOptIn(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "proof-sec-admin")
	const camp = "campaign-proof-sec"
	const token = "proof-sec-token"
	seedFuzzCampaignWithReportToken(t, a, camp, token)
	now := time.Now().Unix()
	// Inject a finding that must NEVER appear on public proof.
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES (?, ?, 'security_violation', 'high', ?, 'aa', '', '', ?, ?)`,
		"finding-secret-leak", camp,
		"detector hit: AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		`{"guard_pack":"secrets","input_hex":"414b4941","explain":"Looks like an AWS access key id"}`,
		now)
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"public_proof": true, "guard_pack": "secrets", "input_mode": "bytes"}
	_, err = db.ExecContext(context.Background(),
		`UPDATE fuzz_campaigns SET status='completed', config_json=?, summary_json=?, completed_at=? WHERE id=?`,
		marshalMapJSON(cfg), marshalMapJSON(map[string]any{"runs_done": 64}), now, camp)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/proof/"+camp+"?format=json", nil)
	rec := httptest.NewRecorder()
	a.handleProofPretty(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, bad := range []string{"AKIA", "input_hex", "ghp_", "AWS_ACCESS", "finding-secret-leak"} {
		if strings.Contains(body, bad) {
			t.Fatalf("public proof leaked %q in: %s", bad, body)
		}
	}
	var proof map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &proof); err != nil {
		t.Fatal(err)
	}
	if _, ok := proof["findings"]; ok {
		t.Fatal("proof must not include findings array")
	}
	// Disclaimer may mention the word "findings" — that is OK; payloads must not appear.

	// Turn off public_proof → anonymous must be rejected
	_, _ = db.ExecContext(context.Background(),
		`UPDATE fuzz_campaigns SET config_json='{}' WHERE id=?`, camp)
	req2 := httptest.NewRequest(http.MethodGet, "/proof/"+camp+"/badge.svg", nil)
	rec2 := httptest.NewRecorder()
	a.handleProofPretty(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("private badge want 401 got %d", rec2.Code)
	}
}

func TestProofOfFuzzPrivateRequiresToken(t *testing.T) {
	a, _ := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "proof-admin-token")
	const camp = "campaign-proof-private"
	const token = "proof-private-token"
	seedFuzzCampaignWithReportToken(t, a, camp, token)

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+camp+"/proof?format=json", nil)
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+camp+"/proof?format=json", nil)
	req2.Header.Set("X-Hackme-Report-Token", token)
	rec2 := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("token proof %d: %s", rec2.Code, rec2.Body.String())
	}
}
