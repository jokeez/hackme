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
			"crash_count": 0, "coverage_noise_count": 1,
			"runs_done": 10, "coverage_edges": 0, "coverage_paths": 0,
			"sample_size": 3,
		},
		"gate": map[string]any{"pass": true, "reasons": []string{"all thresholds satisfied"}},
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
		"baseline_diff":      stubBaselineDiff("no base"),
		"recommendations":    []string{"Review property signals."},
		"fuzz_engine":        map[string]any{"semantics": "detector", "sandbox": "wazero"},
	}
	htmlOut := renderFuzzReportHTML(report)
	for _, want := range []string{
		"fuzz_report_v2",
		"coverage noise",
		"CI gate",
		"Scope &amp; honesty",
		"l1-crypto-stack-v3",
		"property_violation",
	} {
		if !strings.Contains(htmlOut, want) {
			t.Fatalf("missing %q in html", want)
		}
	}
}
