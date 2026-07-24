package workerfuzzloop

import (
	"strings"
	"testing"

	"hackme/internal/chain"
)

func TestPayoutIsTreasury(t *testing.T) {
	if chain.DevFeeAddress == "" {
		t.Fatal("DevFeeAddress unset")
	}
	if !PayoutIsTreasury(chain.DevFeeAddress) {
		t.Fatal("exact treasury address must be refused")
	}
	if !PayoutIsTreasury("  " + chain.DevFeeAddress + "  ") {
		t.Fatal("trimmed treasury must be refused")
	}
	if PayoutIsTreasury("HMC-deadbeefdeadbeef") {
		t.Fatal("non-treasury address must be allowed")
	}
}

func TestTruthyHybridFlags(t *testing.T) {
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "")
	if !HybridFuzzEnabled() {
		t.Fatal("expected hybrid default ON when unset")
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "1")
	if !HybridFuzzEnabled() {
		t.Fatal("expected hybrid enabled")
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "0")
	if HybridFuzzEnabled() {
		t.Fatal("expected hybrid disabled via escape hatch")
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "off")
	if HybridFuzzEnabled() {
		t.Fatal("expected hybrid disabled for off")
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ_MODE", "process")
	if HybridFuzzMode() != "process" {
		t.Fatalf("mode=%s", HybridFuzzMode())
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ_MODE", "")
	if HybridFuzzMode() != "inline" {
		t.Fatalf("default mode=%s", HybridFuzzMode())
	}
}

func TestBackoffForErr(t *testing.T) {
	if d := backoffForErr(errString("HTTP 429 no_fuzz_work")); d < 30e9 {
		t.Fatalf("expected hard backoff, got %v", d)
	}
	if d := backoffForErr(errString("connection refused")); d > 5e9 {
		t.Fatalf("expected soft backoff, got %v", d)
	}
	if d := backoffForErr(errString("HTTP 502 Bad Gateway")); d < 5e9 {
		t.Fatalf("expected gateway backoff >=5s, got %v", d)
	}
}

func TestShortHTTPBody(t *testing.T) {
	html := []byte("<html>\n<head><title>502 Bad Gateway</title></head>\n<body><center><h1>502 Bad Gateway</h1></center></body></html>")
	got := shortHTTPBody(502, html)
	if got != "502 Bad Gateway" {
		t.Fatalf("html title: got %q", got)
	}
	got = shortHTTPBody(503, []byte(`{"error":"busy"}`))
	if got != `{"error":"busy"}` {
		t.Fatalf("json: got %q", got)
	}
	long := strings.Repeat("x", 200)
	got = shortHTTPBody(500, []byte(long))
	if len(got) > 160 || !strings.HasSuffix(got, "...") {
		t.Fatalf("truncate: got len=%d %q", len(got), got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestInputForCheck(t *testing.T) {
	if b := InputForCheck(ClaimResp{}); len(b) != 0 {
		t.Fatal("empty expected")
	}
	b := InputForCheck(ClaimResp{InputBytesHex: "deadbeef"})
	if hex := "deadbeef"; len(b) != 4 || b[0] != 0xde {
		t.Fatalf("got %x want decode of %s", b, hex)
	}
}

func TestEnvInt(t *testing.T) {
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY", "2")
	if EnvInt("HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY", 1) != 2 {
		t.Fatal("env int")
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY", "nope")
	if EnvInt("HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY", 1) != 1 {
		t.Fatal("fallback")
	}
}
