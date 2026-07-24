package workerfuzzloop

import (
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
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "1")
	if !HybridFuzzEnabled() {
		t.Fatal("expected hybrid enabled")
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "0")
	if HybridFuzzEnabled() {
		t.Fatal("expected hybrid disabled")
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
