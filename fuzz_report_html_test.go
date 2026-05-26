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
		"campaign": fuzzCampaign{
			ID: "camp-1", Title: "Test", CampaignType: "property", Status: "done", BudgetRuns: 100,
		},
		"security_summary": map[string]any{
			"confidence": "medium", "vulnerabilities_found": 1,
			"critical_count": 0, "high_count": 0, "medium_count": 1,
			"sample_size": 3,
		},
		"top_issues": []fuzzTopIssue{{
			Severity: "medium", FindingType: "property_violation",
			Title: "check rejected input",
			ReproCmd: `go run ./tools/check_repro -wasm "data/fuzz-artifacts/camp-1/guard.wasm" -input "0x4c210"`,
			Artifact: "/tmp/x.input",
			TriageClass: "expected_signal",
			TriageNote:  "Property signal",
		}},
		"recommendations": []string{"Review property signals."},
		"fuzz_engine": map[string]any{"semantics": "detector", "sandbox": "wazero"},
	}
	html := renderFuzzReportHTML(report)
	for _, want := range []string{
		"fuzz_report_v2",
		"expected_signal",
		"check_repro",
		"Scope &amp; honesty",
		"l1-crypto-stack-v3",
		"Reproduction commands",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in html", want)
		}
	}
}
