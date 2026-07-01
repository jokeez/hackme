package main

import (
	"path/filepath"
	"testing"

	"hackme/internal/fuzzingcli"
)

func TestWizardRefusesPublicBase(t *testing.T) {
	if fuzzingcli.IsLoopbackBase("https://hackme.tech") {
		t.Fatal("hackme.tech must not be loopback")
	}
}

func TestWizardDryRunScanPackage(t *testing.T) {
	wasm := filepath.Join("..", "..", "tasks", "artifacts", "security", "rust_script_push_bounds_guard.wasm")
	m, err := doWizardDryRun("scan", wasm)
	if err != nil {
		t.Fatal(err)
	}
	if m["depth_tier"] != "wasm_only" {
		t.Fatalf("depth_tier=%v", m["depth_tier"])
	}
	if m["pool_distributed"] != false {
		t.Fatalf("scan should be local pool_distributed=false")
	}
}
