package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/workerfuzzloop"
)

func TestHybridFuzzFlagDefaultOn(t *testing.T) {
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "")
	if !workerfuzzloop.HybridFuzzEnabled() {
		t.Fatal("hybrid must default on")
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "0")
	if workerfuzzloop.HybridFuzzEnabled() {
		t.Fatal("hybrid must honor escape hatch =0")
	}
	if startHybridFuzzIfEnabled("http://127.0.0.1:9", "t", "w") != nil {
		t.Fatal("expected nil when flag off")
	}
}

func TestResolveWorkerfuzzBin(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "workerfuzz")
	if err := os.WriteFile(bin, []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_WORKERFUZZ_BIN", bin)
	got := resolveWorkerfuzzBin()
	if got != bin {
		t.Fatalf("got %q want %q", got, bin)
	}
}

func TestHybridDigProfileDefaultOn(t *testing.T) {
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ_DIG", "")
	if !hybridDigProfileEnabled() {
		t.Fatal("dig profile should default ON")
	}
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ_DIG", "0")
	if hybridDigProfileEnabled() {
		t.Fatal("dig profile should honor =0")
	}
}

func TestHybridDigHTTPTimeoutFloor(t *testing.T) {
	t.Setenv("WORKERFUZZ_HTTP_TIMEOUT_SEC", "20")
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ_DIG", "1")
	got := hybridDigHTTPTimeout()
	if got < 30*time.Second {
		t.Fatalf("dig HTTP timeout=%v want >=30s", got)
	}
	t.Setenv("WORKERFUZZ_HTTP_TIMEOUT_SEC", "45")
	if hybridDigHTTPTimeout() != 45*time.Second {
		t.Fatalf("explicit 45s should win")
	}
}
