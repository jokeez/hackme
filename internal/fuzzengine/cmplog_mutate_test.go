package fuzzengine

import (
	"bytes"
	"testing"
)

func TestExtractCmpConstantsBinaryAndJSON(t *testing.T) {
	bin := []byte{0x01, 0x00, 0xff, 0xff, 0x7f, 0x00, 0x00, 0x00}
	json := []byte(`{"id":4294967295,"hex":"deadbeef","n":42}`)
	toks := ExtractCmpConstants(bin, json)
	if len(toks) == 0 {
		t.Fatal("expected tokens from binary and JSON")
	}
	foundDec := false
	foundHex := false
	for _, tok := range toks {
		if bytes.Equal(tok, []byte("4294967295")) || bytes.Equal(tok, []byte("42")) {
			foundDec = true
		}
		if bytes.Equal(tok, []byte("deadbeef")) || bytes.Equal(tok, []byte("dead")) {
			foundHex = true
		}
	}
	if !foundDec {
		t.Fatal("expected decimal run from JSON")
	}
	if !foundHex {
		t.Fatal("expected hex run from JSON")
	}
	if len(toks) > cmpConstCap {
		t.Fatalf("token cap exceeded: %d", len(toks))
	}
}

func TestCmpMutatorsDeterministic(t *testing.T) {
	buf := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	corpus := ExtractCmpConstants([]byte(`{"x":255}`), []byte{0xff, 0x00})
	mix := uint64(0xdeadbeefcafe)

	check := func(name string, a, b []byte) {
		t.Helper()
		if !bytes.Equal(a, b) {
			t.Fatalf("%s not deterministic", name)
		}
	}

	a := cmpReplaceWithInteresting(buf, mix)
	b := cmpReplaceWithInteresting(buf, mix)
	check("cmpReplaceWithInteresting", a, b)

	a = cmpArithTowardInteresting(buf, mix)
	b = cmpArithTowardInteresting(buf, mix)
	check("cmpArithTowardInteresting", a, b)

	a = cmpSwapWithCorpusConst(buf, corpus, mix)
	b = cmpSwapWithCorpusConst(buf, corpus, mix)
	check("cmpSwapWithCorpusConst", a, b)

	a = cmpXorWindow(buf, mix)
	b = cmpXorWindow(buf, mix)
	check("cmpXorWindow", a, b)

	a = cmpInsertBoundary(buf, mix, 4096)
	b = cmpInsertBoundary(buf, mix, 4096)
	check("cmpInsertBoundary", a, b)
}

func TestCmpReplaceChangesBufferWhenLenAtLeast4(t *testing.T) {
	buf := []byte{0x10, 0x20, 0x30, 0x40, 0x50, 0x60}
	for mix := uint64(0); mix < 32; mix++ {
		out := cmpReplaceWithInteresting(buf, mix)
		if bytes.Equal(out, buf) {
			continue
		}
		return
	}
	t.Fatal("cmpReplaceWithInteresting should change buffer for len>=4")
}
