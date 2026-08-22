package fuzzengine

import (
	"strings"
	"testing"
)

func TestRedactSensitiveString(t *testing.T) {
	in := "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE"
	out := RedactSensitiveString(in)
	if out == in {
		t.Fatalf("expected redaction, got %q", out)
	}
	if ContainsSensitivePattern(out) {
		t.Fatalf("redacted output still matches secret pattern: %q", out)
	}
}

func TestRedactKVSecretsNoInfiniteGrowth(t *testing.T) {
	in := "ENV API_SECRET=FAKE_EXAMPLE_DOCKER_SECRET_DO_NOT_USE"
	out := RedactSensitiveString(in)
	if len(out) > len(in)+64 {
		t.Fatalf("redact grew too much: in=%d out=%d %q", len(in), len(out), out)
	}
	if !strings.Contains(out, "SECRET=") {
		t.Fatalf("key should remain: %q", out)
	}
	if strings.Contains(out, "FAKE_EXAMPLE_DOCKER_SECRET_DO_NOT_USE") {
		t.Fatalf("value should be redacted: %q", out)
	}
}

func TestRedactInputForReportBytes(t *testing.T) {
	raw := []byte("GITHUB_PAT=ghp_FAKEEXAMPLETOKENX1234567890123456789")
	hexIn := "4749544855425f5041543d6768705f46414b454558414d504c45544f4b454e5831323334353637383930313233343536373839"
	out := RedactInputForReport(hexIn)
	if out == string(raw) {
		t.Fatal("expected redacted preview")
	}
	if !ContainsSensitivePattern(string(raw)) {
		t.Fatal("test setup: raw should contain secret")
	}
}

func TestRedactInputForReportBinaryTruncated(t *testing.T) {
	long := make([]byte, 40)
	for i := range long {
		long[i] = byte(i)
	}
	hexIn := hexEncode(long)
	out := RedactInputForReport(hexIn)
	if len(out) >= len(hexIn) {
		t.Fatalf("expected truncation for binary: %q", out)
	}
}

func hexEncode(b []byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, v := range b {
		out[i*2] = hexdigits[v>>4]
		out[i*2+1] = hexdigits[v&0x0f]
	}
	return string(out)
}
