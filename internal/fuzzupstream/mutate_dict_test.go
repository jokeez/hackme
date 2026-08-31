package fuzzupstream

import (
	"testing"

	"hackme/internal/fuzzengine"
)

func TestMutateWithDictSplice(t *testing.T) {
	dict := []byte(`nulltrue{"key"}`)
	base := []byte(`{"a":1}`)
	rnd := []byte{70, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	out := MutateWithDict(base, 128, rnd, dict)
	if string(out) == string(base) {
		t.Fatalf("expected mutation got=%q", out)
	}
	// Deterministic for same salt/stage path.
	out2 := MutateWithDict(base, 128, rnd, dict)
	if string(out) != string(out2) {
		t.Fatalf("not deterministic")
	}
	_ = fuzzengine.MutateBytesForHunt(base, fuzzengine.StageHavocBase+12, 99, 128, map[string]any{"mutator_dict": dict}, nil)
}
