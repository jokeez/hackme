package fuzzupstream

import (
	"strings"
	"testing"
)

func TestClassifySanitizerMSAN(t *testing.T) {
	blob := "==1==ERROR: MemorySanitizer: use-of-uninitialized-value\nSUMMARY: MemorySanitizer: use-of-uninitialized-value"
	info := ClassifySanitizer(blob)
	if info.Class != "msan" {
		t.Fatalf("class=%q", info.Class)
	}
	if info.Security {
		t.Fatal("MSAN must not be auto-security/CVE")
	}
	if !strings.Contains(SanitizerDisplayLabel(info), "MSAN") {
		t.Fatalf("label=%q", SanitizerDisplayLabel(info))
	}
}
