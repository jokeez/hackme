package main

import "testing"

func TestCollapseCrashFindingsForReport(t *testing.T) {
	findings := []fuzzFinding{
		{FindingType: "asan", Severity: "critical", Title: "a", Detail: map[string]any{
			"trap": "ERROR: AddressSanitizer: stack-buffer-overflow\nSUMMARY: … in memset",
		}},
		{FindingType: "native_crash", Severity: "critical", Title: "b", Detail: map[string]any{
			"trap": "*** buffer overflow detected ***\n#0 in memset",
		}},
		{FindingType: "asan", Severity: "critical", Title: "c", Detail: map[string]any{
			"trap": "ERROR: AddressSanitizer: stack-buffer-overflow\nSUMMARY: … in memset",
		}},
		{FindingType: "property_violation", Severity: "medium", Title: "noise"},
	}
	display, unique, dup := collapseCrashFindingsForReport(findings)
	if unique != 1 {
		t.Fatalf("unique=%d want 1", unique)
	}
	if dup != 2 {
		t.Fatalf("dup=%d want 2", dup)
	}
	if len(display) != 2 {
		t.Fatalf("display len=%d want 2 (1 crash + 1 noise)", len(display))
	}
}
