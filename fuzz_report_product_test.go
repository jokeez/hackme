package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMoneySpentFromCampaignIgnoresBudget(t *testing.T) {
	c := fuzzCampaign{
		Config:  map[string]any{"budget_hmc": 5.0, "escrow_budget_hmc": 5.0},
		Summary: map[string]any{"spent_hmc": 0.25},
	}
	if got := moneySpentFromCampaign(c); got != 0.25 {
		t.Fatalf("spent=%v want 0.25 (not budget)", got)
	}
	budgetOnly := fuzzCampaign{Config: map[string]any{"budget_hmc": 5.0}}
	if got := moneySpentFromCampaign(budgetOnly); got != 0 {
		t.Fatalf("budget-only must not look spent: %v", got)
	}
	if got := moneySpentFromEscrow(0.1, 0, 0.01); got != 0.11 {
		t.Fatalf("escrow spent=%v", got)
	}
}

func TestPartitionFindingsCrashFirst(t *testing.T) {
	findings := []fuzzFinding{
		{ID: "1", FindingType: "security_violation", Severity: "high", Title: "detector", ReproCmd: "x", InputSHA256: "aa",
			Detail: map[string]any{"guard_pack": "secrets", "input_hex": hex.EncodeToString([]byte("AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"))}},
		{ID: "2", FindingType: "crash", Severity: "critical", Title: "trap", ReproCmd: "go run repro", InputSHA256: "bb", Detail: map[string]any{"actual_input": float64(66)}},
		{ID: "3", FindingType: "property_violation", Severity: "medium", Title: "prop"},
		{ID: "4", FindingType: "hang", Severity: "high", Title: "hung", ReproCmd: "cmd", InputSHA256: "cc"},
	}
	top, noise, crashN, noiseN := partitionFindingsCrashFirst(findings, 5, 25)
	if crashN != 2 || noiseN != 2 {
		t.Fatalf("counts crash=%d noise=%d", crashN, noiseN)
	}
	if len(top) != 2 {
		t.Fatalf("top len=%d want 2", len(top))
	}
	for _, it := range top {
		if it.FindingType == "security_violation" || it.FindingType == "property_violation" {
			t.Fatalf("detector leaked into top: %+v", it)
		}
	}
	if len(noise) != 2 {
		t.Fatalf("noise len=%d", len(noise))
	}
	var packNoise *fuzzProductTopIssue
	for i := range noise {
		if noise[i].ID == "1" {
			packNoise = &noise[i]
			break
		}
	}
	if packNoise == nil || packNoise.GuardPack != "secrets" {
		t.Fatalf("pack noise missing: %+v", packNoise)
	}
	if !strings.Contains(packNoise.Explain, "AWS") {
		t.Fatalf("explain missing AWS guidance: %q", packNoise.Explain)
	}
	if !top[0].Repro.Ready && top[0].ID == "2" {
		// finding 2 has cmd+input — should be ready
	}
	var crashIssue *fuzzProductTopIssue
	for i := range top {
		if top[i].ID == "2" {
			crashIssue = &top[i]
			break
		}
	}
	if crashIssue == nil || !crashIssue.Repro.Ready {
		t.Fatalf("crash repro should be ready: %+v", crashIssue)
	}
}

func TestBuildHumanSummaryAndVerdict(t *testing.T) {
	line := buildHumanSummaryLine(1000, 12, 4, 0, 0)
	if !strings.Contains(line, "1000 runs") || !strings.Contains(line, "no critical") {
		t.Fatalf("bad summary: %s", line)
	}
	card := buildVerdictCard(1000, 0, 0, true, 1.5)
	if card["gate"] != "PASS" {
		t.Fatalf("gate=%v", card["gate"])
	}
	lines, _ := card["lines"].([]string)
	if len(lines) != 5 {
		t.Fatalf("want 5 verdict lines, got %d", len(lines))
	}
}

func TestBuildAssuranceNote(t *testing.T) {
	note := buildAssuranceNote(5000, 0, 0, "crash/hang/ASan/memory")
	if !strings.Contains(note, "Not proven secure") || !strings.Contains(note, "5000 runs") {
		t.Fatalf("bad note: %s", note)
	}
	fail := buildAssuranceNote(100, 1, 0, "")
	if !strings.Contains(fail, "Crash-class findings present") {
		t.Fatalf("bad fail note: %s", fail)
	}
}

func TestBuildTargetFingerprint(t *testing.T) {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}
	sum := sha256.Sum256(wasm)
	want := hex.EncodeToString(sum[:])
	fp := buildTargetFingerprint(map[string]any{
		"wasm_check_hex": hex.EncodeToString(wasm),
	})
	if fp["available"] != true {
		t.Fatalf("expected available: %+v", fp)
	}
	if toString(fp["wasm_sha256"]) != want {
		t.Fatalf("hash=%v want %s", fp["wasm_sha256"], want)
	}
	stub := buildTargetFingerprint(nil)
	if stub["available"] != false {
		t.Fatalf("nil cfg should be unavailable")
	}
}

func TestEstimatePulseETA(t *testing.T) {
	eta := estimatePulseETA(100, 1000, 50, 0, "running")
	if toString(eta["eta_source"]) != "runs_per_sec" {
		t.Fatalf("eta=%+v", eta)
	}
	if intFromAny(eta["eta_sec"]) <= 0 {
		t.Fatalf("expected positive eta: %+v", eta)
	}
	done := estimatePulseETA(1000, 1000, 10, 0, "completed")
	if intFromAny(done["eta_sec"]) != 0 {
		t.Fatalf("completed eta=%+v", done)
	}
}

func TestCrashClassSeverityScore_excludesDetector(t *testing.T) {
	findings := []fuzzFinding{
		{FindingType: "security_violation", Severity: "high"},
		{FindingType: "crash", Severity: "critical"},
	}
	c, h, _, _, _ := crashClassSeverityCounts(findings)
	if c != 1 || h != 0 {
		t.Fatalf("crit=%d high=%d", c, h)
	}
	if crashClassSeverityScore(c, h, 0, 0, 0) != 100 {
		t.Fatal("score should be crash-only")
	}
}

func TestRenderFuzzReportHTML_crashFirstProduct(t *testing.T) {
	wasm := []byte{0x00, 0x61, 0x73, 0x6d}
	sum := sha256.Sum256(wasm)
	report := map[string]any{
		"report_version":    "fuzz_report_v2",
		"generated_at_unix": int64(1_700_000_000),
		"verdict":           "clean",
		"human_summary":     "1000 runs · coverage 3 edges · 1 paths · bugs/crashes 0 crash-class · no critical · no crash-class bugs",
		"assurance_note":    "Not proven secure. None found of crash/hang/ASan/memory at 1000 runs",
		"campaign": fuzzCampaign{
			ID: "camp-1", Title: "Test", CampaignType: "property", Status: "completed", BudgetRuns: 1000,
		},
		"security_summary": map[string]any{
			"confidence": "medium", "vulnerabilities_found": 0,
			"critical_count": 0, "high_count": 0, "medium_count": 0,
			"crash_count": 0, "crash_unique_count": 0, "coverage_noise_count": 1, "coverage_noise_groups": 1,
			"grouped_rows_total": 1, "grouped_rows_visible": 1, "grouped_rows_hidden": 0,
			"hidden_crash_groups": 0, "hidden_noise_groups": 0,
			"fetched_findings": 1, "full_campaign_findings": 1, "history_truncated": false,
			"runs_done": 1000, "coverage_edges": 3, "coverage_paths": 1, "sample_size": 1,
		},
		"gate": map[string]any{
			"pass": true, "reasons": []string{"all thresholds satisfied"},
			"thresholds": map[string]any{
				"max_critical": 0, "max_high": 0, "max_severity_score": 0, "min_runs_done": 0, "min_sample_size": 0,
			},
			"observed": map[string]any{
				"raw_findings_total": 1, "grouped_rows_total": 1, "grouped_rows_visible": 1, "grouped_rows_hidden": 0,
				"crash_count": 0, "crash_unique_count": 0, "hidden_crash_groups": 0,
				"coverage_noise_count": 1, "coverage_noise_groups": 1, "hidden_noise_groups": 0,
				"interesting_count": 0, "interesting_groups": 0, "crash_repro_ready": 0, "crash_repro_gap": 0,
				"fetched_findings": 1, "full_campaign_findings": 1, "history_truncated": false,
				"crash_family_raw":     map[string]int{"crash": 0, "hang": 0, "trap": 0},
				"crash_family_grouped": map[string]int{"crash": 0, "hang": 0, "trap": 0},
			},
		},
		"verdict_card": map[string]any{
			"lines": []string{"Runs: 1000", "Crashes: 0", "Critical: 0", "Gate: PASS", "Money spent: 1.0000 HMC"},
			"gate":  "PASS", "gate_pass": true,
		},
		"top_issues": []fuzzProductTopIssue{},
		"coverage_noise": []fuzzProductTopIssue{{
			Severity: "high", FindingType: "security_violation", Title: "detector flagged",
			TriageClass: "coverage_noise",
		}},
		"target_fingerprint": map[string]any{
			"available": true, "wasm_sha256": hex.EncodeToString(sum[:]), "source": "sha256(wasm_check_hex)",
		},
		"evidence_window": map[string]any{
			"query_limit": 500, "fetched_findings": 1, "full_campaign_findings": 1, "history_truncated": false,
		},
		"baseline_diff":   stubBaselineDiff("set config.base_campaign_id to enable baseline diff"),
		"recommendations": []string{"No crash findings."},
		"fuzz_engine":     map[string]any{"check_semantics": "detector", "sandbox": "wazero"},
	}
	htmlOut := renderFuzzReportHTML(report)
	for _, want := range []string{
		"fuzz_report_v2",
		"CI gate",
		"PASS",
		"Verdict card",
		"Human summary",
		"coverage noise",
		"Target fingerprint",
		"Baseline diff",
		"Not proven secure",
		"crash / hang / ASan / memory only",
		"Scope &amp; honesty",
		"Fetched window",
		"Shown rows",
		"report request fetched 1 findings (limit 500) versus 1 total findings in campaign history",
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("missing %q in html", want)
		}
	}
	if strings.Contains(htmlOut, "detector flagged") && !strings.Contains(htmlOut, "Appendix") {
		t.Fatal("detector should only appear under appendix")
	}
}

func TestBuildFuzzReportEvidenceWindowAndCSV(t *testing.T) {
	a, db := newWalletTestApp(t)
	const camp = "report-window-api"
	const token = "report-window-api-token"
	seedFuzzCampaignWithReportToken(t, a, camp, token)
	now := time.Now().Unix()
	rows := []fuzzFinding{
		{ID: "rw-1", CampaignID: camp, FindingType: "crash", Severity: "critical", Title: "latest trap", InputSHA256: "aa", ReproCmd: "go run repro latest", CreatedAt: now + 3},
		{ID: "rw-2", CampaignID: camp, FindingType: "property_violation", Severity: "medium", Title: "latest noise", InputSHA256: "bb", CreatedAt: now + 2},
		{ID: "rw-3", CampaignID: camp, FindingType: "crash", Severity: "high", Title: "older trap", InputSHA256: "cc", ReproCmd: "go run repro older", CreatedAt: now + 1},
		{ID: "rw-4", CampaignID: camp, FindingType: "property_violation", Severity: "medium", Title: "older noise", InputSHA256: "dd", CreatedAt: now},
	}
	for _, f := range rows {
		_, err := db.ExecContext(context.Background(),
			`INSERT INTO fuzz_findings
			 (id, campaign_id, finding_type, severity, title, input_sha256, artifact_path, repro_cmd, detail_json, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, '', ?, '{}', ?)`,
			f.ID, f.CampaignID, f.FindingType, f.Severity, f.Title, f.InputSHA256, f.ReproCmd, f.CreatedAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	report, err := a.buildFuzzReport(context.Background(), camp, 2)
	if err != nil {
		t.Fatal(err)
	}
	window, _ := report["evidence_window"].(map[string]any)
	if intFromAny(window["query_limit"]) != 2 || intFromAny(window["fetched_findings"]) != 2 || intFromAny(window["full_campaign_findings"]) != 4 {
		t.Fatalf("window mismatch: %+v", window)
	}
	if truncated, _ := window["history_truncated"].(bool); !truncated {
		t.Fatalf("expected truncated evidence window: %+v", window)
	}
	summary, _ := report["security_summary"].(map[string]any)
	if intFromAny(summary["raw_findings_total"]) != 2 || intFromAny(summary["fetched_findings"]) != 2 || intFromAny(summary["full_campaign_findings"]) != 4 {
		t.Fatalf("summary evidence window mismatch: %+v", summary)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+camp+"/report.csv?limit=2", nil)
	req.Header.Set("X-Hackme-Report-Token", token)
	rec := httptest.NewRecorder()
	a.handleFuzzCampaigns(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	reader := csv.NewReader(strings.NewReader(rec.Body.String()))
	reader.FieldsPerRecord = -1
	if _, err := reader.ReadAll(); err != nil {
		t.Fatal(err)
	}
	joined := rec.Body.String()
	for _, want := range []string{
		"summary,raw_findings_total,2",
		"summary,grouped_rows_visible,2",
		"summary,fetched_findings,2",
		"summary,full_campaign_findings,4",
		"summary,history_truncated,true",
		"gate_observed,fetched_findings,2",
		"gate_observed,full_campaign_findings,4",
		"evidence_window,query_limit,2",
		"evidence_window,history_truncated,true",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("csv missing %q\n%s", want, joined)
		}
	}
}
