package fuzzupstream

import "testing"

func TestMutateWithDictSplice(t *testing.T) {
	dict := []byte(`{"key":true}`)
	base := []byte(`x`)
	rnd := []byte{0, 3, 1, 2, 3, 4, 5, 6}
	out := MutateWithDict(base, 64, rnd, dict)
	if len(out) <= len(base) {
		t.Fatalf("expected growth got=%q", out)
	}
}
