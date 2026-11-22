package fuzzengine

import (
	"bytes"
	"testing"
)

func TestMutateBytesDeterministic(t *testing.T) {
	base := U64LayoutToBytes(0x4c | (521 << 8))
	a := MutateBytes(base, 0, 99, 4096)
	b := MutateBytes(base, 0, 99, 4096)
	if string(a) != string(b) {
		t.Fatal("MutateBytes not deterministic")
	}
}

func TestPublicWASMMutateClampsWhileV210UsesNativeCeil(t *testing.T) {
	if MaxInputBytesHardCeil != 4096 || NativeHuntMutateCeil != 65536 {
		t.Fatalf("ceils wasm=%d native=%d", MaxInputBytesHardCeil, NativeHuntMutateCeil)
	}
	base := bytes.Repeat([]byte("A"), 5000)
	huge := bytes.Repeat([]byte("B"), 70000)
	if n := len(MutateBytes(base, 0, 1, 100000)); n > 4096 {
		t.Fatalf("MutateBytes len=%d", n)
	}
	v210 := map[string]any{"havoc_deep_v210": true}
	if n := len(MutateBytesForConfig(huge, 0, 1, 100000, v210)); n != 4096 {
		t.Fatalf("WASM MutateBytesForConfig honored v210: len=%d", n)
	}
	if n := len(MutateBytesWithDict(huge, 0, 1, 100000, nil)); n != 4096 {
		t.Fatalf("MutateBytesWithDict len=%d want 4096", n)
	}
	if n := len(MutateBytesForHunt(huge, 0, 1, 100000, nil, nil)); n != 4096 {
		t.Fatalf("hunt without v210 len=%d want 4096", n)
	}
	got := MutateBytesForHunt(base, 0, 1, 100000, v210, nil)
	if len(got) <= 4096 || len(got) > NativeHuntMutateCeil {
		t.Fatalf("v210 5k seed len=%d want (4096, 65536]", len(got))
	}
	capped := MutateBytesForHunt(huge, 0, 1, 100000, v210, nil)
	if len(capped) > NativeHuntMutateCeil || len(capped) <= 4096 {
		t.Fatalf("v210 ceil len=%d", len(capped))
	}
}

func TestDeterministicStagesCoverBands(t *testing.T) {
	base := []byte(`{"a":1,"b":[2,3,4]}`)
	seen := map[string]struct{}{}
	for stage := 0; stage < StageDeterministicMax; stage++ {
		out := MutateBytes(base, MutationStage(stage), 0xC0FFEE, 4096)
		if string(out) == string(base) {
			t.Fatalf("stage %d produced identical output", stage)
		}
		seen[string(out)] = struct{}{}
		// Replay stability across calls.
		again := MutateBytes(base, MutationStage(stage), 0xC0FFEE, 4096)
		if string(out) != string(again) {
			t.Fatalf("stage %d not replay-stable", stage)
		}
	}
	if len(seen) < 32 {
		t.Fatalf("expected diverse deterministic outputs, got %d unique", len(seen))
	}
}

func TestGuidedBytesUsesCorpus(t *testing.T) {
	cfg := map[string]any{"input_mode": "bytes"}
	violation := U64LayoutToBytes(PackWasmCheckInput(0x4c, 521, 0))
	seeds := []PoolCorpusSeed{{Input: PackInputBytesToU64(violation), Energy: 3}}
	linear, _ := GuidedInputForWork(11, cfg, nil)
	guided, b := GuidedInputForWork(11, cfg, seeds)
	if len(b) == 0 {
		t.Fatal("expected byte payload")
	}
	if PackInputBytesToU64(b) == linear && guided == linear {
		t.Fatal("guided bytes should differ from linear fallback")
	}
}
