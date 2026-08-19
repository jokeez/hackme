package main

import (
	"strings"
	"testing"
)

func TestRenderFuzzReportHTML_v2ReproAndTriage(t *testing.T) {
	report := map[string]any{
		"report_version":    "fuzz_report_v2",
		"generated_at_unix": int64(1_700_000_000),
		"verdict":           "warn_medium",
		"human_summary":     "10 runs · coverage 0 edges · 0 paths · bugs/crashes 0 crash-class · no critical",
		"assurance_note":    "Not proven secure.",
		"campaign": fuzzCampaign{
			ID: "camp-1", Title: "Test", CampaignType: "property", Status: "done", BudgetRuns: 100,
		},
		"security_summary": map[string]any{
			"confidence": "medium", "vulnerabilities_found": 0,
			"critical_count": 0, "high_count": 0, "medium_count": 0,
			"crash_count": 0, "crash_unique_count": 0, "coverage_noise_count": 1, "coverage_noise_groups": 1,
			"grouped_rows_total": 1, "grouped_rows_visible": 1, "grouped_rows_hidden": 0,
			"hidden_crash_groups": 0, "hidden_noise_groups": 0,
			"fetched_findings": 3, "full_campaign_findings": 3, "history_truncated": false,
			"runs_done": 10, "coverage_edges": 0, "coverage_paths": 0,
			"sample_size": 3,
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
				"fetched_findings": 3, "full_campaign_findings": 3, "history_truncated": false,
				"crash_family_raw":     map[string]int{"crash": 0, "hang": 0, "trap": 0},
				"crash_family_grouped": map[string]int{"crash": 0, "hang": 0, "trap": 0},
			},
		},
		"verdict_card": map[string]any{
			"lines": []string{"Runs: 10", "Crashes: 0", "Critical: 0", "Gate: PASS", "Money spent: 0.0000 HMC"},
		},
		"top_issues": []fuzzProductTopIssue{},
		"coverage_noise": []fuzzProductTopIssue{{
			Severity: "medium", FindingType: "property_violation",
			Title:       "check rejected input",
			ReproCmd:    `go run ./tools/check_repro -wasm "data/fuzz-artifacts/camp-1/guard.wasm" -input "0x4c210"`,
			Artifact:    "/tmp/x.input",
			TriageClass: "coverage_noise",
			TriageNote:  "Coverage noise",
		}},
		"target_fingerprint": map[string]any{"available": false, "note": "stub"},
		"evidence_window": map[string]any{
			"query_limit": 500, "fetched_findings": 3, "full_campaign_findings": 3, "history_truncated": false,
		},
		"baseline_diff":   stubBaselineDiff("no base"),
		"recommendations": []string{"Review property signals."},
		"fuzz_engine":     map[string]any{"semantics": "detector", "sandbox": "wazero"},
	}
	htmlOut := renderFuzzReportHTML(report)
	for _, want := range []string{
		"fuzz_report_v2",
		"coverage noise",
		"CI gate",
		"Scope &amp; honesty",
		"l1-crypto-stack-v3",
		"Fetched window",
		"Shown rows",
		"report request fetched 3 findings (limit 500) versus 3 total findings in campaign history",
		"property_violation",
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("missing %q in html", want)
		}
	}
}

func TestRenderFuzzReportHTML_windowTruncationDisclosure(t *testing.T) {
	report := map[string]any{
		"report_version":    "fuzz_report_v2",
		"generated_at_unix": int64(1_700_000_000),
		"verdict":           "warn_medium",
		"human_summary":     "2 runs · coverage 0 edges · 0 paths · bugs/crashes 1 crash-class · no critical",
		"assurance_note":    "Not proven secure.",
		"campaign": fuzzCampaign{
			ID: "camp-2", Title: "Window", CampaignType: "property", Status: "done", BudgetRuns: 100,
		},
		"security_summary": map[string]any{
			"critical_count": 0, "high_count": 1, "medium_count": 0,
			"crash_count": 1, "crash_unique_count": 1, "coverage_noise_count": 1, "coverage_noise_groups": 1,
			"grouped_rows_total": 2, "grouped_rows_visible": 2, "grouped_rows_hidden": 0,
			"hidden_crash_groups": 0, "hidden_noise_groups": 0,
			"fetched_findings": 2, "full_campaign_findings": 4, "history_truncated": true,
			"runs_done": 2, "coverage_edges": 0, "coverage_paths": 0, "sample_size": 2,
		},
		"gate": map[string]any{
			"pass": false, "reasons": []string{"crash high_count exceeds threshold"},
			"thresholds": map[string]any{
				"max_critical": 0, "max_high": 0, "max_severity_score": 0, "min_runs_done": 0, "min_sample_size": 0,
			},
			"observed": map[string]any{
				"raw_findings_total": 2, "grouped_rows_total": 2, "grouped_rows_visible": 2, "grouped_rows_hidden": 0,
				"crash_count": 1, "crash_unique_count": 1, "hidden_crash_groups": 0,
				"coverage_noise_count": 1, "coverage_noise_groups": 1, "hidden_noise_groups": 0,
				"interesting_count": 0, "interesting_groups": 0, "crash_repro_ready": 1, "crash_repro_gap": 0,
				"fetched_findings": 2, "full_campaign_findings": 4, "history_truncated": true,
				"crash_family_raw":     map[string]int{"crash": 1, "hang": 0, "trap": 0},
				"crash_family_grouped": map[string]int{"crash": 1, "hang": 0, "trap": 0},
			},
		},
		"verdict_card": map[string]any{
			"lines": []string{"Runs: 2", "Crashes: 1", "Critical: 0", "Gate: FAIL", "Money spent: 0.0000 HMC"},
		},
		"top_issues": []fuzzProductTopIssue{{
			Severity: "high", FindingType: "crash", Title: "latest trap",
			Repro: fuzzReproBlock{Command: "go run repro latest", Ready: true},
		}},
		"coverage_noise": []fuzzProductTopIssue{{
			Severity: "medium", FindingType: "property_violation", Title: "latest noise",
		}},
		"target_fingerprint": map[string]any{"available": false, "note": "stub"},
		"evidence_window": map[string]any{
			"query_limit": 2, "fetched_findings": 2, "full_campaign_findings": 4, "history_truncated": true,
		},
		"baseline_diff":   stubBaselineDiff("no base"),
		"recommendations": []string{"Review crash finding."},
		"fuzz_engine":     map[string]any{"semantics": "detector", "sandbox": "wazero"},
	}
	htmlOut := renderFuzzReportHTML(report)
	for _, want := range []string{
		"Fetched window</b>2 fetched / 4 history / truncated true",
		"report request fetched 2 findings (limit 2) versus 4 total findings in campaign history",
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("missing %q in html", want)
		}
	}
}
