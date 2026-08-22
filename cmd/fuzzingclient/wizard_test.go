package main

import (
	"path/filepath"
	"testing"

	"hackme/internal/fuzzengine"
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
	if m["create_poh_order"] != false {
		t.Fatalf("scan should not create PoH")
	}
	sigs, _ := m["signal_types"].([]string)
	if len(sigs) == 0 || sigs[0] != "wasm_smoke" {
		t.Fatalf("scan signals=%v", m["signal_types"])
	}
}

func TestWizardDryRunPackSecrets(t *testing.T) {
	m, err := doWizardDryRunPack("audit", "secrets", "")
	if err != nil {
		t.Fatal(err)
	}
	if m["pack"] != "secrets" {
		t.Fatalf("pack=%v", m["pack"])
	}
	if m["input_mode"] != "bytes" {
		t.Fatalf("input_mode=%v", m["input_mode"])
	}
	if m["guided_scheduling"] != true {
		t.Fatal("expected guided")
	}
	if m["depth_tier"] != string(fuzzengine.DepthBytesCorpus) {
		t.Fatalf("depth_tier=%v", m["depth_tier"])
	}
	if m["wasm_len"].(int) < 100 {
		t.Fatalf("wasm too small: %v", m["wasm_len"])
	}
}

func TestWizardDryRunPackagesDiffer(t *testing.T) {
	wasm := filepath.Join("..", "..", "tasks", "artifacts", "security", "rust_script_push_bounds_guard.wasm")
	scan, err := doWizardDryRun("scan", wasm)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := doWizardDryRun("audit", wasm)
	if err != nil {
		t.Fatal(err)
	}
	deep, err := doWizardDryRun("deep", wasm)
	if err != nil {
		t.Fatal(err)
	}
	if scan["depth_tier"] == audit["depth_tier"] || audit["depth_tier"] == deep["depth_tier"] {
		t.Fatalf("tiers must differ: scan=%v audit=%v deep=%v", scan["depth_tier"], audit["depth_tier"], deep["depth_tier"])
	}
	if deep["depth_tier"] != string(fuzzengine.DepthBytesCorpus) {
		t.Fatalf("deep depth_tier=%v", deep["depth_tier"])
	}
	if deep["budget_runs"].(int) <= audit["budget_runs"].(int) {
		t.Fatalf("deep runs must exceed audit: deep=%v audit=%v", deep["budget_runs"], audit["budget_runs"])
	}
	if deep["budget_seconds"].(int) < 3600*8 {
		t.Fatalf("deep should be hours-scale budget_seconds, got %v", deep["budget_seconds"])
	}
	if deep["mutation_rounds"].(int) < 8 {
		t.Fatalf("deep should use heavy mutation, got %v", deep["mutation_rounds"])
	}
	if deep["coverage_guided"] != true {
		t.Fatal("deep should be coverage_guided")
	}
	deepSigs, _ := deep["signal_types"].([]string)
	auditSigs, _ := audit["signal_types"].([]string)
	if len(deepSigs) <= len(auditSigs) {
		t.Fatalf("deep signals should exceed audit: deep=%v audit=%v", deepSigs, auditSigs)
	}
}
