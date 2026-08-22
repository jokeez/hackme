package fuzzengine

import "testing"

func TestMutateInputDeterministic(t *testing.T) {
	base := uint64(0x4c | (521 << 8))
	a := MutateInput(base, 0, 42)
	b := MutateInput(base, 0, 42)
	if a != b {
		t.Fatalf("mutate not deterministic: %x vs %x", a, b)
	}
	if a == base {
		t.Fatal("stage 0 bit flip should change input")
	}
}

func TestGuidedInputForWorkUsesCorpus(t *testing.T) {
	cfg := map[string]any{"mutation_rounds": 4}
	seeds := []PoolCorpusSeed{{Input: 0x4c | (521 << 8), Energy: 3}}
	linear, _ := GuidedInputForWork(7, cfg, nil)
	guided, _ := GuidedInputForWork(7, cfg, seeds)
	if guided == linear {
		t.Fatalf("guided should differ from linear fallback: %x", guided)
	}
	g2, _ := GuidedInputForWork(7, cfg, seeds)
	if guided != g2 {
		t.Fatal("guided input must be stable for same input_n")
	}
}
