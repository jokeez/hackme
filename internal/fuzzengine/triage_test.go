package fuzzengine

import (
	"strings"
	"testing"
)

func TestClassifyFinding_property(t *testing.T) {
	tr := ClassifyFinding("property_violation", "medium")
	if tr.Class != "expected_signal" || tr.ZeroDayHint != "low" {
		t.Fatalf("unexpected triage: %+v", tr)
	}
}

func TestReproCmdTool_withWasm(t *testing.T) {
	cmd := ReproCmdTool("/tmp/guard.wasm", 0x42)
	if cmd == "" || !strings.Contains(cmd, "check_repro") {
		t.Fatalf("bad repro cmd: %q", cmd)
	}
}
