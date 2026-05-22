package gputune

import (
	"strings"
	"testing"
)

func TestClassifyGPUFailure_ChaosMatrix(t *testing.T) {
	cases := []struct {
		kind string
		want GPUFailureClass
	}{
		{"vram", FailureVRAM},
		{"oom", FailureVRAM},
		{"tdr", FailureTDR},
		{"thermal", FailureThermal},
		{"driver", FailureDriver},
		{"timeout", FailureTimeout},
		{"unknown", FailureUnknown},
	}
	for _, tc := range cases {
		err := SimulateChaosFailure(tc.kind)
		got := ClassifyGPUFailure(err)
		if got != tc.want {
			t.Fatalf("%s: got %q want %q", tc.kind, got, tc.want)
		}
		if !ShouldCPUFallback(got) {
			t.Fatalf("%s: expected cpu fallback", tc.kind)
		}
	}
}

func TestShouldCPUFallback_None(t *testing.T) {
	if ShouldCPUFallback(FailureNone) {
		t.Fatal("nil error class should not fallback")
	}
	if ShouldCPUFallback(ClassifyGPUFailure(nil)) {
		t.Fatal("nil err")
	}
}

func TestFormatWorkerGPUEvent_JSON(t *testing.T) {
	line := FormatWorkerGPUEvent("worker-test", "cuda", FailureVRAM, SimulateChaosFailure("vram"))
	if line == "" || line[0] != '{' {
		t.Fatalf("json line: %q", line)
	}
	for _, needle := range []string{`"event":"gpu_fallback"`, `"fallback":"cpu_claim_chunk"`, `"session_preserved":true`} {
		if !strings.Contains(line, needle) {
			t.Fatalf("missing %q in %s", needle, line)
		}
	}
}
