package main

import "testing"

func TestBuildFindingFamilySummaryCollapse(t *testing.T) {
	findings := []fuzzFinding{
		{ID: "1", FindingType: "asan", Severity: "critical", Title: "a", InputSHA256: "sha-a", Detail: map[string]any{
			"trap": "ERROR: AddressSanitizer: stack-buffer-overflow\nSUMMARY: … in memset",
		}},
		{ID: "2", FindingType: "native_crash", Severity: "critical", Title: "b", InputSHA256: "sha-b", Detail: map[string]any{
			"trap": "*** buffer overflow detected ***\n#0 in memset",
		}},
		{ID: "3", FindingType: "asan", Severity: "critical", Title: "c", InputSHA256: "sha-c", Detail: map[string]any{
			"trap": "ERROR: AddressSanitizer: stack-buffer-overflow\nSUMMARY: … in memset",
		}},
		// Duplicate input SHA for same family must not inflate counts.
		{ID: "3b", FindingType: "asan", Severity: "critical", Title: "c-dup", InputSHA256: "sha-c", Detail: map[string]any{
			"trap": "ERROR: AddressSanitizer: stack-buffer-overflow\nSUMMARY: … in memset",
		}},
		{ID: "4", FindingType: "ubsan", Severity: "medium", Title: "u", InputSHA256: "sha-u", Detail: map[string]any{
			"trap": "runtime error: call to function through pointer to incorrect function type\nSUMMARY: function-pointer-cast ucl_hash.c:275",
		}},
		{ID: "5", FindingType: "property_violation", Severity: "low", Title: "noise"},
	}
	sum := buildFindingFamilySummary(findings)
	if intFromAny(sum["family_count"]) != 2 {
		t.Fatalf("family_count=%v want 2 (memset + fn_ptr)", sum["family_count"])
	}
	if intFromAny(sum["raw_input_count"]) != 4 {
		t.Fatalf("raw=%v want 4 unique inputs", sum["raw_input_count"])
	}
	cr := sum["collapse_ratio"].(float64)
	if cr < 0.4 {
		t.Fatalf("collapse_ratio=%.2f too low", cr)
	}
	issues := []fuzzProductTopIssue{toProductTopIssue(findings[0])}
	annotateTopIssuesWithFamilyCounts(issues, sum)
	if issues[0].FindingFamily == "" || issues[0].FamilyCount < 2 {
		t.Fatalf("annotate failed: %+v", issues[0])
	}
}

func TestRenderFuzzFamilySection(t *testing.T) {
	html := renderFuzzFamilySection(map[string]any{
		"finding_families": map[string]any{
			"family_count":    1,
			"raw_input_count": 65,
			"collapse_ratio":  0.98,
			"honesty_note":    "Cite family_count",
			"top_families": []map[string]any{
				{"family": "crash|fn_ptr_cast|ucl", "inputs": 65},
			},
		},
	})
	if html == "" || !containsStr(html, "Finding families") || !containsStr(html, "65") {
		t.Fatalf("html=%s", html)
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
