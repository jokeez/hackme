package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Writes static fuzz_report_v2 HTML snapshots under web/site/reports/portfolio/deliverables/
// when HACKME_WRITE_PORTFOLIO_REPORTS=1. Used by the public research ledger "Show report" UI.
func TestWritePortfolioDeliverableSnaps(t *testing.T) {
	if os.Getenv("HACKME_WRITE_PORTFOLIO_REPORTS") != "1" {
		t.Skip("set HACKME_WRITE_PORTFOLIO_REPORTS=1 to regenerate portfolio deliverable HTML")
	}
	outDir := filepath.Join("web", "site", "reports", "portfolio", "deliverables")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	type snap struct {
		file string
		rep  map[string]any
	}
	genTrap := int64(time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC).Unix())
	genExiv := int64(time.Date(2026, 11, 9, 11, 30, 0, 0, time.UTC).Unix())
	genOni := int64(time.Date(2026, 11, 8, 18, 0, 0, 0, time.UTC).Unix())

	snaps := []snap{
		{
			file: "traplib.html",
			rep: map[string]any{
				"report_version":    "fuzz_report_v2",
				"generated_at_unix": genTrap,
				"verdict":           "fail_critical",
				"human_summary":     "665+ pool shards · 1 crash-class · 1 critical ASAN heap-buffer-overflow (0xA3 OOB-read path) · positive-control target",
				"assurance_note":    "Calibration case: bugs were planted on purpose. Attested pool ASAN ≠ CVE claim on third-party code.",
				"campaign": fuzzCampaign{
					ID: "hunt-traplib-asan-20261109t065811z", Title: "rc19 traplib ASAN positive-control",
					CampaignType: "hunt", Status: "done", BudgetRuns: 4000,
				},
				"security_summary": map[string]any{
					"runs_done": "~665+", "crash_count": 1, "crash_unique_count": 1, "crash_duplicate_count": 0,
					"critical_count": 1, "high_count": 0, "coverage_noise_count": 0, "sanitizer_hygiene_count": 0,
					"raw_findings_total": 1, "grouped_rows_visible": 1, "grouped_rows_hidden": 0,
					"fetched_findings": 1, "full_campaign_findings": 1, "coverage_edges": "—", "coverage_paths": "—",
				},
				"gate": map[string]any{
					"pass": false, "reasons": []string{"critical_count 1 exceeds max_critical 0"},
				},
				"verdict_card": map[string]any{
					"lines": []string{
						"Runs: ~665+", "Crashes: 1", "Critical: 1", "Gate: FAIL",
						"Money spent: ~60 HMC (hunt_standard)", "SKU: hunt_standard · pool_distributed",
					},
				},
				"top_issues": []fuzzProductTopIssue{{
					Severity: "critical", FindingType: "native_crash",
					Title:       "Hunt ASAN · heap-buffer-overflow on inv_fuzzing_traplib_fuzzer_c",
					TriageClass: "needs_triage",
					TriageNote:  "Intentional positive-control OOB-read (byte0=0xA3, size≥2)",
					ReproCmd:    "printf '\\xA3\\x41' | ASAN_OPTIONS=abort_on_error=1 ./harness.bin",
					Repro: fuzzReproBlock{
						Command: "printf '\\xA3\\x41' | ASAN_OPTIONS=abort_on_error=1 ./harness.bin",
						Ready:   true,
					},
					SanitizerClass: "asan", SanitizerSubtype: "heap-buffer-overflow",
					FindingFamily: "asan/heap-buffer-overflow",
				}},
				"finding_families": map[string]any{
					"family_count": 1, "raw_input_count": 1, "collapse_ratio": "1.00",
					"honesty_note": "Single planted family; not a customer product finding.",
					"top_families": []map[string]any{{"family": "asan/heap-buffer-overflow", "inputs": 1}},
				},
				"coverage_noise": []fuzzProductTopIssue{},
				"evidence_window": map[string]any{
					"query_limit": 500, "fetched_findings": 1, "full_campaign_findings": 1, "history_truncated": false,
				},
				"target_fingerprint": map[string]any{
					"available": true, "binary_sha256": "51a38ea6243d92e4e6b4482d8ecc78cfee7720d22ab8e748ae1b08e3bda67fde",
					"source": "harness_content_sha256",
				},
				"baseline_diff": stubBaselineDiff("positive-control — no baseline campaign"),
				"recommendations": []string{
					"Keep traplib as a permanent CI / pool calibration target.",
					"Do not file a CVE — bugs are intentional.",
				},
				"fuzz_engine": map[string]any{
					"check_semantics": "native_crash", "depth_tier": "oss_cve",
					"hunt_package": "hunt_standard", "hunt_sanitizer_profile": "asan+ubsan+lsan",
				},
			},
		},
		{
			file: "exiv2.html",
			rep: map[string]any{
				"report_version":    "fuzz_report_v2",
				"generated_at_unix": genExiv,
				"verdict":           "clean",
				"human_summary":     "3561 pool shards verified · 0 crash-class · 0 critical · Ubuntu libexiv2 0.27.6 custom inventory harness · CLEAN",
				"assurance_note":    "CLEAN is a budget / window statement — not a proof Exiv2 is free of bugs. Tip 0.28.x is a follow-up (P04).",
				"campaign": fuzzCampaign{
					ID: "hunt-exiv2-bb-20261109t011326z", Title: "rc19 custom Exiv2 BB case",
					CampaignType: "hunt", Status: "cancelled", BudgetRuns: 4000,
				},
				"security_summary": map[string]any{
					"runs_done": 3561, "crash_count": 0, "crash_unique_count": 0, "crash_duplicate_count": 0,
					"critical_count": 0, "high_count": 0, "coverage_noise_count": 0, "sanitizer_hygiene_count": 0,
					"raw_findings_total": 0, "grouped_rows_visible": 0, "grouped_rows_hidden": 0,
					"fetched_findings": 0, "full_campaign_findings": 0, "coverage_edges": "—", "coverage_paths": "—",
				},
				"gate": map[string]any{
					"pass": true, "reasons": []string{"all thresholds satisfied (max_critical=0, max_high=0)"},
				},
				"verdict_card": map[string]any{
					"lines": []string{
						"Runs: 3561", "Crashes: 0", "Critical: 0", "Gate: PASS",
						"Money spent: ~60 HMC (hunt_standard)", "Harness: custom inventory (not catalog)",
					},
				},
				"top_issues":    []fuzzProductTopIssue{},
				"coverage_noise": []fuzzProductTopIssue{},
				"evidence_window": map[string]any{
					"query_limit": 500, "fetched_findings": 0, "full_campaign_findings": 0, "history_truncated": false,
				},
				"target_fingerprint": map[string]any{
					"available": true, "binary_sha256": "f76ad99e4043ea7c3c26a9f9508ec6de69dd78266ffbbc233929eac143c1c367",
					"source": "harness_content_sha256 · Ubuntu libexiv2 0.27.6",
				},
				"baseline_diff": stubBaselineDiff("first custom inventory Exiv2 soak — no baseline"),
				"recommendations": []string{
					"Graduate to Exiv2 0.28.x source tree for modern ASF/QuickTime parsers (P04).",
					"CLEAN ≠ GHSA-free — keep watching advisories before claiming depth.",
				},
				"fuzz_engine": map[string]any{
					"check_semantics": "native_crash", "depth_tier": "oss_cve",
					"hunt_package": "hunt_standard", "hunt_sanitizer_profile": "asan+ubsan+lsan",
				},
			},
		},
		{
			file: "oniguruma.html",
			rep: map[string]any{
				"report_version":    "fuzz_report_v2",
				"generated_at_unix": genOni,
				"verdict":           "clean",
				"human_summary":     "Two hunt_standard catalog soaks (~4k shards each, ~8k combined) · 0 crash-class · 0 critical · CLEAN×2",
				"assurance_note":    "Mature catalog target + smoke SKU often stays CLEAN. Pairs with traplib Found so the ledger shows both outcomes.",
				"campaign": fuzzCampaign{
					ID: "hunt-oniguruma-20261108t152836z", Title: "oniguruma catalog soak (2nd)",
					CampaignType: "hunt", Status: "done", BudgetRuns: 4000,
				},
				"security_summary": map[string]any{
					"runs_done": "~8k", "crash_count": 0, "crash_unique_count": 0, "crash_duplicate_count": 0,
					"critical_count": 0, "high_count": 0, "coverage_noise_count": 0, "sanitizer_hygiene_count": 0,
					"raw_findings_total": 0, "grouped_rows_visible": 0, "grouped_rows_hidden": 0,
					"fetched_findings": 0, "full_campaign_findings": 0, "coverage_edges": "—", "coverage_paths": "—",
				},
				"gate": map[string]any{
					"pass": true, "reasons": []string{"all thresholds satisfied across both soaks"},
				},
				"verdict_card": map[string]any{
					"lines": []string{
						"Campaigns: hunt-oniguruma-20261108t152210z + …t152836z",
						"Runs: ~8k combined", "Crashes: 0", "Critical: 0", "Gate: PASS",
						"SKU: hunt_standard · catalog",
					},
				},
				"top_issues":    []fuzzProductTopIssue{},
				"coverage_noise": []fuzzProductTopIssue{},
				"evidence_window": map[string]any{
					"query_limit": 500, "fetched_findings": 0, "full_campaign_findings": 0, "history_truncated": false,
				},
				"target_fingerprint": map[string]any{"available": false, "note": "catalog target — pin hash on coordinator"},
				"baseline_diff":      stubBaselineDiff("catalog soak pair — no cross-baseline"),
				"recommendations": []string{
					"Keep oniguruma as a fleet depth / catalog smoke lane.",
					"Rotate mutator / deeper SKU if the goal shifts from smoke to disclosure hunt.",
				},
				"fuzz_engine": map[string]any{
					"check_semantics": "native_crash", "depth_tier": "oss_cve",
					"hunt_package": "hunt_standard", "hunt_sanitizer_profile": "asan+ubsan+lsan",
				},
			},
		},
	}

	for _, s := range snaps {
		htmlOut := renderFuzzReportHTML(s.rep)
		path := filepath.Join(outDir, s.file)
		if err := os.WriteFile(path, []byte(htmlOut), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s (%d bytes)", path, len(htmlOut))
	}
}
