package main

import (
	"os"
	"path/filepath"
	"testing"

	"hackme/internal/workerfuzzloop"
)

func TestHybridFuzzFlagDefaultOff(t *testing.T) {
	t.Setenv("HACKME_WORKER_HYBRID_FUZZ", "")
	if workerfuzzloop.HybridFuzzEnabled() {
		t.Fatal("hybrid must default off")
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
