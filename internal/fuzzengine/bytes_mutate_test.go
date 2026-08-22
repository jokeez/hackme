package fuzzengine

import "testing"

func TestMutateBytesDeterministic(t *testing.T) {
	base := U64LayoutToBytes(0x4c | (521 << 8))
	a := MutateBytes(base, 0, 99, 4096)
	b := MutateBytes(base, 0, 99, 4096)
	if string(a) != string(b) {
		t.Fatal("MutateBytes not deterministic")
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
