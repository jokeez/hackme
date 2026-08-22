package main

import (
	"strings"
	"testing"
)

func TestBuildFindingReproRedactsSecrets(t *testing.T) {
	f := fuzzFinding{
		FindingType: "security_violation",
		Severity:    "high",
		Detail: map[string]any{
			"input_hex": "4749544855425f5041543d6768705f46414b454558414d504c45544f4b454e5831323334353637383930313233343536373839",
		},
	}
	repro := buildFindingRepro(f)
	if repro.InputHex == "" {
		t.Fatal("expected input preview")
	}
	if repro.InputHex == "4749544855425f5041543d6768705f46414b454558414d504c45544f4b454e5831323334353637383930313233343536373839" {
		t.Fatalf("raw secret hex leaked: %q", repro.InputHex)
	}
}

func TestToProductTopIssueRedactsTitle(t *testing.T) {
	f := fuzzFinding{
		FindingType: "security_violation",
		Severity:    "high",
		Title:       "detector hit: AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		Detail: map[string]any{
			"guard_pack": "secrets",
			"input_hex":  "414b4941494f53464f444e4e374558414d504c45",
		},
	}
	issue := toProductTopIssue(f)
	if strings.Contains(issue.Title, "AKIAIOSFODNN7EXAMPLE") {
		t.Fatalf("title not redacted: %q", issue.Title)
	}
	if issue.Explain == "" || !strings.Contains(issue.Explain, "AWS") {
		t.Fatalf("explain should still classify AWS: %q", issue.Explain)
	}
}
