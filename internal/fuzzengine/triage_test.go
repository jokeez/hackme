package fuzzengine

import (
	"strings"
	"testing"
)

func TestClassifyFinding_property(t *testing.T) {
	tr := ClassifyFinding("property_violation", "medium")
	if tr.ZeroDayHint != "low" {
		t.Fatalf("unexpected triage: %+v", tr)
	}
	if tr.Class != "expected_signal" && tr.Class != "coverage_noise" {
		t.Fatalf("want expected_signal or coverage_noise, got %q", tr.Class)
	}
}

func TestClassifyFinding_crashClasses(t *testing.T) {
	for _, ft := range []string{"crash", "hang", "asan", "memory_error", "native_crash", "timeout_hang"} {
		tr := ClassifyFinding(ft, "high")
		if tr.Class != "needs_triage" {
			t.Fatalf("%s: want needs_triage, got %+v", ft, tr)
		}
		if !IsCrashClass(ft) {
			t.Fatalf("%s should be crash-class", ft)
		}
	}
}

func TestClassifyFinding_harnessRuntime(t *testing.T) {
	tr := ClassifyFinding("harness_runtime", "info")
	if tr.Class != "harness_noise" || tr.ZeroDayHint != "low" {
		t.Fatalf("unexpected triage: %+v", tr)
	}
	if IsCrashClass("harness_runtime") {
		t.Fatal("harness_runtime must not be crash-class")
	}
	if !IsCoverageNoise("harness_runtime") {
		t.Fatal("harness_runtime should be treated as non-crash noise")
	}
}

func TestClassifyWasmTrap_harness(t *testing.T) {
	ft, sev, title := ClassifyWasmTrap(0, "source module must be compiled before instantiation", true)
	if ft != "harness_runtime" || sev != "info" {
		t.Fatalf("got %s/%s %q", ft, sev, title)
	}
	if !IsHarnessRuntimeTrap("module has already been closed") {
		t.Fatal("expected closed-module detection")
	}
}

func TestIsCoverageNoise_detector(t *testing.T) {
	for _, ft := range []string{"property_violation", "security_violation", "consensus_script_push", "interesting_input"} {
		if !IsCoverageNoise(ft) {
			t.Fatalf("%s should be coverage noise", ft)
		}
		if IsCrashClass(ft) {
			t.Fatalf("%s must not be crash-class", ft)
		}
	}
	if IsCoverageNoise("crash") {
		t.Fatal("crash must not be coverage noise")
	}
}

func TestReproCmdTool_withWasm(t *testing.T) {
	cmd := ReproCmdTool("/tmp/guard.wasm", 0x42)
	if cmd == "" || !strings.Contains(cmd, "check_repro") {
		t.Fatalf("bad repro cmd: %q", cmd)
	}
}
