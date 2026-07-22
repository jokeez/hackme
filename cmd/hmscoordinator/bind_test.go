package main

import (
	"testing"
)

func TestHmsBindLoopbackOnlyEmptyHost(t *testing.T) {
	if hmsBindLoopbackOnly(":18082") {
		t.Fatal("empty host must not count as loopback")
	}
	if hmsBindLoopbackOnly("0.0.0.0:18082") {
		t.Fatal("0.0.0.0 must not count as loopback")
	}
	if !hmsBindLoopbackOnly("127.0.0.1:18082") {
		t.Fatal("127.0.0.1 must be loopback")
	}
}

func TestStratumBridgeAllowedFailClosed(t *testing.T) {
	t.Setenv("HMS_STRATUM_ENABLE", "1")
	t.Setenv("HMS_STRATUM_INSECURE", "")
	t.Setenv("HMS_STRATUM_WORKER_HMAC_SECRET", "")
	ok, reason := stratumBridgeAllowed(":3334", "0.0.0.0:18082")
	if ok || reason == "" {
		t.Fatalf("public stratum without HMAC must fail closed: ok=%v reason=%q", ok, reason)
	}
	t.Setenv("HMS_STRATUM_INSECURE", "1")
	ok, reason = stratumBridgeAllowed(":3334", "0.0.0.0:18082")
	if ok {
		t.Fatalf("INSECURE on public bind must fail: reason=%q", reason)
	}
	t.Setenv("HMS_STRATUM_INSECURE", "")
	t.Setenv("HMS_STRATUM_WORKER_HMAC_SECRET", "secret")
	ok, reason = stratumBridgeAllowed(":3334", "0.0.0.0:18082")
	if !ok {
		t.Fatalf("HMAC on public bind should allow start: %s", reason)
	}
	t.Setenv("HMS_STRATUM_WORKER_HMAC_SECRET", "")
	ok, reason = stratumBridgeAllowed("127.0.0.1:3334", "127.0.0.1:18082")
	if !ok {
		t.Fatalf("loopback stratum without HMAC is allowed for pilot: %s", reason)
	}
}
