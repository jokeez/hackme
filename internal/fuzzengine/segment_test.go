package fuzzengine

import (
	"context"
	"testing"
)

func TestExecPerUnitDefaults(t *testing.T) {
	if ExecPerUnit(map[string]any{"depth_tier": "wasm_only"}) != 1 {
		t.Fatal("scan default")
	}
	if ExecPerUnit(map[string]any{"depth_tier": "wasm_native"}) != defaultExecPerUnitAudit {
		t.Fatal("audit default")
	}
	if ExecPerUnit(map[string]any{"depth_tier": "bytes_corpus"}) != defaultExecPerUnitDeep {
		t.Fatal("deep default")
	}
	if ExecPerUnit(map[string]any{"exec_per_unit": 99999}) != maxExecPerUnitHardCeil {
		t.Fatal("clamp max")
	}
}

func TestSegmentExecInputDeterministic(t *testing.T) {
	cfg := map[string]any{
		"input_mode":       "bytes",
		"seed_byte_corpus": []any{"41414141", "42424242"},
		"exec_per_unit":    16,
	}
	for execIdx := uint64(0); execIdx < 8; execIdx++ {
		_, a := SegmentExecInput(42, execIdx, cfg, nil)
		_, b := SegmentExecInput(42, execIdx, cfg, nil)
		if string(a) != string(b) {
			t.Fatalf("exec %d not deterministic", execIdx)
		}
	}
	_, anchor := SegmentExecInput(42, 0, cfg, nil)
	_, later := SegmentExecInput(42, 7, cfg, nil)
	if string(anchor) == string(later) {
		t.Fatal("expected different inputs across exec indices")
	}
}

func TestEvalSegmentFindsDetectorHit(t *testing.T) {
	cfg := map[string]any{
		"input_mode":      "bytes",
		"check_semantics":   "detector",
		"exec_per_unit":     4,
		"seed_byte_corpus":  []any{"00"},
		"max_input_bytes":   64,
	}
	sem := ParseCheckSemantics(cfg)
	res := EvalSegment(context.Background(), 1, cfg, nil, sem, func(_ context.Context, _ uint64, inputB []byte) (int32, string, error, []byte) {
		if len(inputB) > 0 && inputB[0] == 0xff {
			return 1, "", nil, nil
		}
		return 0, "", nil, nil
	})
	if res.ExecDone != 4 {
		t.Fatalf("exec_done=%d", res.ExecDone)
	}
	if res.UniqueEdgeSeen < 1 {
		t.Fatal("expected edge buckets")
	}
}

func TestCoverageKindHonestDefault(t *testing.T) {
	if CoverageKind(nil) != "input_fingerprint" {
		t.Fatal(CoverageKind(nil))
	}
}
