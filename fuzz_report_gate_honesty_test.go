package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func seedCustomerFuzzCampaignWithConfig(
	t *testing.T,
	a *app,
	db *sql.DB,
	campaignID string,
	reportToken string,
	cfg map[string]any,
	runsDone int,
	status string,
) {
	t.Helper()
	seedFuzzCampaignWithReportToken(t, a, campaignID, reportToken)

	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sumJSON, err := json.Marshal(map[string]any{"runs_done": runsDone})
	if err != nil {
		t.Fatal(err)
	}

	if status == "" {
		status = "completed"
	}
	_, err = db.ExecContext(context.Background(),
		`UPDATE fuzz_campaigns SET config_json=?, summary_json=?, status=? WHERE id=?`,
		string(cfgJSON), string(sumJSON), status, campaignID)
	if err != nil {
		t.Fatal(err)
	}
}

func insertFuzzFinding(
	t *testing.T,
	db *sql.DB,
	campaignID string,
	findingID string,
	findingType string,
	severity string,
	title string,
	inputSHA256 string,
	reproCmd string,
	createdAt time.Time,
) {
	insertFuzzFindingDetail(t, db, campaignID, findingID, findingType, severity, title, inputSHA256, reproCmd, "{}", createdAt)
}

func insertFuzzFindingDetail(
	t *testing.T,
	db *sql.DB,
	campaignID string,
	findingID string,
	findingType string,
	severity string,
	title string,
	inputSHA256 string,
	reproCmd string,
	detailJSON string,
	createdAt time.Time,
) {
	t.Helper()
	_, err := db.ExecContext(context.Background(),
		`INSERT INTO fuzz_findings
		 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, '', ?, ?, ?)`,
		findingID, campaignID, findingType, severity, title, inputSHA256, reproCmd, detailJSON, createdAt.Unix())
	if err != nil {
		t.Fatal(err)
	}
}

func getGate(
	t *testing.T,
	a *app,
	campaignID string,
	reportToken string,
	query string,
) map[string]any {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+campaignID+"/gate?"+query, nil)
	req.Header.Set("X-Hackme-Report-Token", reportToken)
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("gate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func getReport(
	t *testing.T,
	a *app,
	campaignID string,
	limit int,
) map[string]any {
	t.Helper()
	r, err := a.buildFuzzReport(context.Background(), campaignID, limit)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestFuzzGateHonesty_NoiseOnly_MultiDepthTier(t *testing.T) {
	ctx := context.Background()
	tiers := []struct {
		name           string
		depthTierValue string
	}{
		{name: "wasm_only", depthTierValue: "wasm_only"},
		{name: "wasm_native", depthTierValue: "wasm_native"},
		{name: "bytes_corpus", depthTierValue: "bytes_corpus"},
	}

	for _, tier := range tiers {
		t.Run(tier.name, func(t *testing.T) {
			a, db := newWalletTestApp(t)
			const token = "gate-honesty-token"
			camp := cleanFuzzID("gate-noise-only-"+tier.name, "campaign")
			seedCustomerFuzzCampaignWithConfig(t, a, db, camp, token,
				map[string]any{"depth_tier": tier.depthTierValue}, 123, "completed")

			now := time.Now()
			// Latest two entries have "critical" severity but are still detector/property coverage noise.
			insertFuzzFinding(t, db, camp, "n1", "property_violation", "critical", "noise-critical", "aa", "", now.Add(10*time.Minute))
			insertFuzzFinding(t, db, camp, "n2", "security_violation", "critical", "noise-security-critical", "bb", "", now.Add(5*time.Minute))
			insertFuzzFinding(t, db, camp, "n3", "property_violation", "high", "noise-high", "cc", "", now.Add(1*time.Minute))

			for _, limit := range []int{2, 5} {
				// Default max_* thresholds should allow clean/no-crash to PASS.
				gate := getGate(t, a, camp, token, "max_critical=0&max_high=0&max_severity_score=0&min_sample_size=0&min_runs_done=0&limit="+
					toString(limit))

				if gate["pass"] != true {
					t.Fatalf("limit=%d gate should PASS for noise-only: %+v", limit, gate)
				}

				obs, _ := gate["observed"].(map[string]any)
				if intFromAny(obs["critical_count"]) != 0 || intFromAny(obs["high_count"]) != 0 {
					t.Fatalf("limit=%d crash counters must be 0 for noise-only, got %+v", limit, obs)
				}

				// Evidence window sanity: when asking for limit=2, fetched_findings must be 2 (if at least 2 rows exist).
				if intFromAny(obs["fetched_findings"]) != minInt(limit, 3) {
					t.Fatalf("limit=%d fetched_findings=%v want %d", limit, obs["fetched_findings"], minInt(limit, 3))
				}
				if got, ok := obs["history_truncated"].(bool); ok {
					// We only truncate when requested limit is below total findings.
					if limit < 3 && got != true {
						t.Fatalf("limit=%d expected history_truncated=true", limit)
					}
					if limit >= 3 && got != false {
						t.Fatalf("limit=%d expected history_truncated=false", limit)
					}
				}

				// Depth-tier correctness: engine meta must reflect the configured tier.
				rep := getReport(t, a, camp, limit)
				engineMeta, _ := rep["fuzz_engine"].(map[string]any)
				if got := toString(engineMeta["depth_tier"]); got != tier.depthTierValue {
					t.Fatalf("limit=%d depth_tier=%q want %q", limit, got, tier.depthTierValue)
				}
			}
			_ = ctx
		})
	}
}

func TestFuzzGateHonesty_MixedCrashNoise_EvidenceWindowCaps(t *testing.T) {
	type tierCase struct {
		name           string
		depthTierValue string
	}
	tiers := []tierCase{
		{name: "wasm_only", depthTierValue: "wasm_only"},
		{name: "wasm_native", depthTierValue: "wasm_native"},
		{name: "bytes_corpus", depthTierValue: "bytes_corpus"},
	}

	for _, tc := range tiers {
		t.Run("mixed-"+tc.name, func(t *testing.T) {
			a, db := newWalletTestApp(t)
			const token = "gate-honesty-token"
			camp := cleanFuzzID("gate-mixed-crash-noise-"+tc.name, "campaign")
			seedCustomerFuzzCampaignWithConfig(t, a, db, camp, token,
				map[string]any{"depth_tier": tc.depthTierValue}, 1000, "completed")

			now := time.Now()
			// Newest entries (top of evidence window) are noise-only, so limit=2 should PASS.
			insertFuzzFinding(t, db, camp, "noise-1", "property_violation", "critical", "noise-critical-1", "aa1", "", now.Add(30*time.Minute))
			insertFuzzFinding(t, db, camp, "noise-2", "security_violation", "high", "noise-high-2", "aa2", "", now.Add(25*time.Minute))

			// Older entry is a crash-class issue; including it makes limit=5 FAIL.
			insertFuzzFinding(t, db, camp, "crash-1", "crash", "critical", "boom-critical-1", "cc1", "go run repro crash-1", now.Add(10*time.Minute))

			// Fill remaining history so that limit=5 truncation is still meaningful.
			insertFuzzFinding(t, db, camp, "noise-3", "property_violation", "medium", "noise-medium-3", "aa3", "", now.Add(15*time.Minute))
			insertFuzzFinding(t, db, camp, "noise-4", "interesting_input", "low", "noise-interesting-4", "aa4", "", now.Add(5*time.Minute))
			insertFuzzFinding(t, db, camp, "noise-5", "sandbox_reject", "critical", "noise-sandbox-reject-5", "aa5", "", now.Add(1*time.Minute))

			// Total findings inserted: 6.
			for _, limit := range []int{2, 5} {
				gate := getGate(t, a, camp, token,
					"max_critical=0&max_high=0&max_severity_score=0&min_sample_size=0&min_runs_done=0&limit="+toString(limit))
				obs, _ := gate["observed"].(map[string]any)

				fetched := intFromAny(obs["fetched_findings"])
				wantFetched := minInt(limit, 6)
				if fetched != wantFetched {
					t.Fatalf("limit=%d fetched_findings=%d want %d", limit, fetched, wantFetched)
				}

				trunc, _ := obs["history_truncated"].(bool)
				if limit < 6 && trunc != true {
					t.Fatalf("limit=%d expected history_truncated=true", limit)
				}
				if limit >= 6 && trunc != false {
					t.Fatalf("limit=%d expected history_truncated=false", limit)
				}

				criticalCount := intFromAny(obs["critical_count"])
				if limit == 2 {
					if gate["pass"] != true || criticalCount != 0 {
						t.Fatalf("limit=2 should PASS with noise-only fetched window; critical_count=%d gate=%+v", criticalCount, gate)
					}
				} else if limit == 5 {
					// Evidence window includes crash-1 when limit >= 5.
					if gate["pass"] != false || criticalCount == 0 {
						t.Fatalf("limit=5 should FAIL once crash-class is in evidence window; critical_count=%d gate=%+v", criticalCount, gate)
					}
				}
			}
		})
	}
}

func TestFuzzGateHonesty_RawCrashCountersNotGroupedDisplay(t *testing.T) {
	a, db := newWalletTestApp(t)
	const token = "gate-honesty-token"
	camp := "gate-raw-not-grouped"
	seedCustomerFuzzCampaignWithConfig(t, a, db, camp, token,
		map[string]any{"depth_tier": "wasm_native"}, 42, "completed")

	now := time.Now()
	// Insert 7 distinct crash buckets (different trap text) so dedup keeps 7 rows; display topLimit=5 hides 2.
	for i := 1; i <= 7; i++ {
		detail := `{"trap":"ERROR: AddressSanitizer: heap-buffer-overflow on variant ` + toString(i) + `\nSUMMARY: … in crash` + toString(i) + `"}`
		insertFuzzFindingDetail(t, db, camp,
			"crash-"+toString(i), "crash", "critical", "dup-crash-title", "in"+toString(i),
			"go run repro crash-"+toString(i), detail, now.Add(time.Duration(i)*time.Minute))
	}

	// Ask for evidence window size 7; gate should fail based on raw critical_count=7.
	gate := getGate(t, a, camp, token,
		"max_critical=0&max_high=0&max_severity_score=0&min_sample_size=0&min_runs_done=0&limit=7")

	obs, _ := gate["observed"].(map[string]any)
	if gate["pass"] != false {
		t.Fatalf("expected FAIL when raw crash-class exceeds thresholds: %+v", gate)
	}
	if intFromAny(obs["critical_count"]) != 7 {
		t.Fatalf("critical_count=%d want 7 (raw crash counters)", intFromAny(obs["critical_count"]))
	}

	// Grouped display should cap visible rows at 5 (topIssues) and hide the rest.
	// Gate observed includes grouped_rows_visible/hidden to let us prove it's not using grouped display for pass/fail.
	visible := intFromAny(obs["grouped_rows_visible"])
	hidden := intFromAny(obs["grouped_rows_hidden"])
	if visible != 5 || hidden != 2 {
		t.Fatalf("grouped_rows_visible=%d grouped_rows_hidden=%d want 5/2", visible, hidden)
	}
}

func TestFuzzGateHonesty_DuplicateFindingsAffectCrashCounters(t *testing.T) {
	a, db := newWalletTestApp(t)
	const token = "gate-honesty-token"
	camp := "gate-duplicate-crash-counts"
	seedCustomerFuzzCampaignWithConfig(t, a, db, camp, token,
		map[string]any{"depth_tier": "bytes_corpus"}, 77, "completed")

	now := time.Now()

	// Duplicate crash-class issues: same type/title/severity, different IDs.
	insertFuzzFinding(t, db, camp, "dup-crash-1", "crash", "critical", "dup-crash-title", "in-dup-1", "go run repro dup1", now.Add(4*time.Minute))
	insertFuzzFinding(t, db, camp, "dup-crash-2", "crash", "critical", "dup-crash-title", "in-dup-2", "go run repro dup2", now.Add(3*time.Minute))

	// Some noise in the window, to ensure gate is still crash-class based.
	insertFuzzFinding(t, db, camp, "noise-1", "property_violation", "critical", "noise-critical", "in-noise-1", "", now.Add(2*time.Minute))
	insertFuzzFinding(t, db, camp, "noise-2", "security_violation", "high", "noise-high", "in-noise-2", "", now.Add(1*time.Minute))

	// limit=4 includes both duplicates.
	gate := getGate(t, a, camp, token,
		"max_critical=0&max_high=0&max_severity_score=0&min_sample_size=0&min_runs_done=0&limit=4")
	if gate["pass"] != false {
		t.Fatalf("expected FAIL when duplicate crash-class findings exceed thresholds: %+v", gate)
	}
	obs, _ := gate["observed"].(map[string]any)
	if intFromAny(obs["critical_count"]) != 2 {
		t.Fatalf("critical_count=%d want 2 (duplicates must count)", intFromAny(obs["critical_count"]))
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

