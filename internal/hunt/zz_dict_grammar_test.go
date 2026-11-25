package hunt

import (
	"strings"
	"testing"
)

// A single-file parser yields no source tokens and falls back to binary noise. The
// classifier has to recognise the format from id or title so the grammar can seed what
// the parser actually branches on. It must stay silent when nothing is unambiguous —
// a wrong grammar is the noise this is meant to remove.
func TestClassifyTargetFormat(t *testing.T) {
	cases := []struct {
		id, title string
		src       []string
		want      string
	}{
		{"jsmn", "zserge/jsmn · jsmn_parse", nil, "json"},
		{"parson", "kgabis/parson · json_parse_string", nil, "json"}, // id alone has no "json"
		{"cj5", "septag/cj5 · cj5_parse JSON5", nil, "json5"},        // not plain json
		{"tomlc99", "cktan/tomlc99 · toml_parse", nil, "toml"},
		{"mxml", "michaelrsweet/mxml · mxmlLoadString", nil, "xml"},
		{"tinycbor", "intel/tinycbor · cbor_parser_init", nil, "cbor"},
		{"mpack", "ludocode/mpack · mpack_reader", nil, "msgpack"}, // mpack → msgpack, not cbor
		{"cyaml", "tlsa/libcyaml · cyaml_load", nil, "yaml"},
		// Author-named parser, no format word anywhere, but its sources carry the keywords.
		{"frozen", "cesanta/frozen · json_scanf", []string{"null", "true", "false", "obj"}, "json"},
		// Not a structured-text format: must stay silent, not be force-fed JSON.
		{"minimp3", "lieff/minimp3 · mp3dec_decode_frame", nil, ""},
		{"uriparser", "uriparser/uriparser · uriParseSingleUriA", nil, ""},
		{"zlib", "madler/zlib · inflate", nil, ""},
	}
	for _, c := range cases {
		if got := classifyTargetFormat(c.id, c.title, c.src); got != c.want {
			t.Errorf("classify(%q,%q) = %q, want %q", c.id, c.title, got, c.want)
		}
	}
}

func TestFormatGrammarTokens(t *testing.T) {
	// JSON grammar must carry the structural bytes a tokenizer branches on.
	toks, format := FormatGrammarTokens("jsmn", "zserge/jsmn", nil)
	if format != "json" {
		t.Fatalf("format = %q, want json", format)
	}
	joined := " " + strings.Join(toks, " ") + " "
	for _, must := range []string{" { ", " } ", " [ ", " ] ", " : ", " null ", " true "} {
		if !strings.Contains(joined, must) {
			t.Errorf("json grammar missing %q", strings.TrimSpace(must))
		}
	}
	// An unknown format yields nothing, so the caller behaves exactly as before.
	if toks, format := FormatGrammarTokens("minimp3", "lieff/minimp3", nil); toks != nil || format != "" {
		t.Fatalf("unknown format returned %v/%q, want nil/empty", toks, format)
	}

	// Binary grammars must survive dictionary formatting: the \x escapes have to come out
	// quoted, or libFuzzer rejects the whole dict (the bug that aborted a dict earlier).
	cbor, _ := FormatGrammarTokens("tinycbor", "intel/tinycbor cbor", nil)
	out := string(AugmentLibFuzzerDict(nil, cbor, "grammar:cbor"))
	if !strings.Contains(out, `"\xff"`) {
		t.Fatalf("cbor break byte not quoted in dict:\n%s", out)
	}
	if strings.Contains(out, "\x00") {
		t.Fatal("a raw NUL reached the dictionary bytes")
	}
}

// The grammar must be additive: a target with real source tokens keeps them and gains the
// structural ones, never trades one for the other.
func TestGrammarIsAdditive(t *testing.T) {
	src := []string{"yajl_specific_token", "parse_integer"}
	grammar, format := FormatGrammarTokens("parson", "kgabis/parson · json_parse_string", src)
	if format != "json" {
		t.Fatalf("format = %q", format)
	}
	merged := append(append([]string(nil), src...), grammar...)
	joined := strings.Join(merged, " ")
	if !strings.Contains(joined, "yajl_specific_token") {
		t.Fatal("source token was dropped when grammar was added")
	}
	if !strings.Contains(joined, "null") {
		t.Fatal("grammar token was not added")
	}
}
