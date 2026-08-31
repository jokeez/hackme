package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"hackme/internal/fuzzescrow"
)

func seedHuntCampaignWithFinding(
	t *testing.T,
	a *app,
	campaignID, reportToken string,
	runsDone int,
) {
	t.Helper()
	seedFuzzCampaignWithReportToken(t, a, campaignID, reportToken)
	cfg := map[string]any{
		"work_kind":          "hunt_shard",
		"campaign_type":      "hunt",
		"upstream_target_id": "jsmn",
		"escrow_split":       fuzzescrow.EscrowSplit5050,
		"check_semantics":    "native_crash",
		"depth_tier":         "oss_cve",
		"hunt_package":       "hunt_lite",
		"bounty_requires_native": true,
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sumJSON, err := json.Marshal(map[string]any{"runs_done": runsDone})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.db.ExecContext(context.Background(),
		`UPDATE fuzz_campaigns SET campaign_type='hunt', title=?, budget_runs=?, status='running', config_json=?, summary_json=? WHERE id=?`,
		"Hunt report e2e", 1200, string(cfgJSON), string(sumJSON), campaignID)
	if err != nil {
		t.Fatal(err)
	}
}

// TestHuntReportE2ENativeCrashFindingToHTML seeds a Hunt campaign + native_crash row,
// builds the product report, and fetches token-gated HTML over HTTP.
func TestHuntReportE2ENativeCrashFindingToHTML(t *testing.T) {
	a, db := newWalletTestApp(t)
	t.Setenv("HACKME_ADMIN_TOKEN", "hunt-report-e2e-admin")
	const camp = "hunt-report-e2e-camp"
	const token = "hunt-report-e2e-customer-token"
	seedHuntCampaignWithFinding(t, a, camp, token, 12)

	now := time.Now()
	detail := `{"trap":"hunt_crash:heap-buffer-overflow","upstream_target_id":"jsmn","miner_address":"HMC-1234567890123456","input_hex":"6372617368"}`
	insertFuzzFindingDetail(t, db, camp, "hunt-find-1", "native_crash", "critical",
		"Hunt ASAN heap-buffer-overflow on jsmn", "abc123deadbeef", "ASAN_OPTIONS=detect_leaks=0 ./harness.bin < crash.bin",
		detail, now)

	ctx := context.Background()
	report, err := a.buildFuzzReport(ctx, camp, 50)
	if err != nil {
		t.Fatal(err)
	}
	c, ok := report["campaign"].(fuzzCampaign)
	if !ok || !strings.EqualFold(c.CampaignType, "hunt") {
		t.Fatalf("campaign type=%v", report["campaign"])
	}
	if toString(report["verdict"]) != "fail_critical" {
		t.Fatalf("verdict=%v want fail_critical", report["verdict"])
	}
	issues := productTopIssues(report)
	if len(issues) == 0 || issues[0].FindingType != "native_crash" {
		t.Fatalf("top_issues=%+v", issues)
	}
	if !strings.Contains(toString(report["human_summary"]), "shards verified") {
		t.Fatalf("human_summary=%q", report["human_summary"])
	}

	htmlOut := renderFuzzReportHTML(report)
	for _, want := range []string{
		"Scope &amp; honesty · Hunt",
		"native_crash",
		"heap-buffer-overflow",
		"FAIL",
		" · hunt · ",
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("html missing %q", want)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+camp+"/report.html", nil)
	req.Header.Set("X-Hackme-Report-Token", token)
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("report.html status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "native_crash") {
		t.Fatalf("HTTP html missing native_crash")
	}
	if !strings.Contains(rec.Body.String(), "Scope &amp; honesty · Hunt") {
		t.Fatalf("HTTP html missing hunt scope")
	}

	gate := getGate(t, a, camp, token, "max_critical=0&max_high=0")
	if gate["pass"] == true {
		t.Fatalf("gate must FAIL on hunt native_crash critical: %+v", gate)
	}

	reqJSON := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+camp+"/report?format=json&limit=10", nil)
	reqJSON.Header.Set("X-Hackme-Report-Token", token)
	recJSON := httptest.NewRecorder()
	a.handleFuzzCampaigns(recJSON, reqJSON)
	if recJSON.Code != http.StatusOK {
		t.Fatalf("report json status=%d", recJSON.Code)
	}
	var j map[string]any
	if err := json.Unmarshal(recJSON.Body.Bytes(), &j); err != nil {
		t.Fatal(err)
	}
	if toString(j["verdict"]) != "fail_critical" {
		t.Fatalf("json verdict=%v", j["verdict"])
	}
}
