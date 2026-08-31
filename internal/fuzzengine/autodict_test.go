package fuzzengine

import "testing"

func TestExtractAutodictTokensJSON(t *testing.T) {
	toks := ExtractAutodictTokens([]byte(`{"userId":1,"name":"x"}`))
	if len(toks) == 0 {
		t.Fatal("expected tokens")
	}
	found := false
	for _, tok := range toks {
		if string(tok) == "userId" {
			found = true
		}
	}
	if !found {
		t.Fatalf("tokens=%v", toks)
	}
}

func TestMergeAutodictPreservesStatic(t *testing.T) {
	static := []byte(`{"null"`)
	got := MergeAutodict(static, [][]byte{[]byte("userId")})
	if len(got) <= len(static) {
		t.Fatalf("got=%q", got)
	}
}

func TestMutateBytesInterestingDeterministic(t *testing.T) {
	base := []byte(`{"a":1}`)
	cfg := map[string]any{"mutator_dict": []byte(`nulltrue`)}
	corpus := [][]byte{[]byte(`{"userId":99}`)}
	a := MutateBytesForHunt(base, StageHavocBase+7, 42, 128, cfg, corpus)
	b := MutateBytesForHunt(base, StageHavocBase+7, 42, 128, cfg, corpus)
	if string(a) != string(b) {
		t.Fatalf("not deterministic a=%q b=%q", a, b)
	}
	if string(a) == string(base) {
		t.Fatalf("expected mutation")
	}
}

func TestParseDictTokens(t *testing.T) {
	toks := ParseDictTokens([]byte(`nulltrue{"key"}`))
	if len(toks) < 2 {
		t.Fatalf("tokens=%v", toks)
	}
}
