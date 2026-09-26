package gpudig

import (
	"testing"

	"hackme/internal/fuzzengine"
)

func TestGenerateMutantsDeterministic(t *testing.T) {
	base := []byte(`{"a":1}`)
	cfg := map[string]any{"dig_gpu_mutators": true, "havoc_deep_v28": true}
	a := GenerateMutants(base, 8, 42, 256, cfg, nil)
	b := GenerateMutants(base, 8, 42, 256, cfg, nil)
	if len(a) != 8 || len(b) != 8 {
		t.Fatalf("len a=%d b=%d", len(a), len(b))
	}
	for i := range a {
		if string(a[i]) != string(b[i]) {
			t.Fatalf("non-deterministic at %d", i)
		}
	}
}

func TestGenerateMutantsDifferFromBase(t *testing.T) {
	base := []byte(`hello-world-payload`)
	cfg := map[string]any{"dig_gpu_mutators": true}
	out := GenerateMutants(base, 16, 7, 128, cfg, [][]byte{[]byte(`other`)})
	changed := 0
	for _, m := range out {
		if string(m) != string(base) {
			changed++
		}
		if len(m) == 0 || len(m) > 128 {
			t.Fatalf("bad len %d", len(m))
		}
	}
	if changed < 8 {
		t.Fatalf("expected most mutants to differ, changed=%d", changed)
	}
	_ = fuzzengine.Version
}

func TestEnabled(t *testing.T) {
	if Enabled(nil) || Enabled(map[string]any{}) {
		t.Fatal("default off")
	}
	if !Enabled(map[string]any{"dig_gpu_mutators": true}) {
		t.Fatal("want on")
	}
}
