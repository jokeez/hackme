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
