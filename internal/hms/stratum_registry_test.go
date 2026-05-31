package hms

import "testing"

func TestParseReportedTH(t *testing.T) {
	if v := parseReportedTH("x"); v != 0 {
		t.Fatalf("x: %v", v)
	}
	if v := parseReportedTH("th=120"); v != 120 {
		t.Fatalf("th=120: %v", v)
	}
	if v := parseReportedTH("1.5th"); v != 1.5 {
		t.Fatalf("1.5th: %v", v)
	}
}
