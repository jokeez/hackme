package hunt

import (
	"strings"
	"testing"
)

func TestMutatorDictForTargetJSON(t *testing.T) {
	d := MutatorDictForTarget("cjson")
	if len(d) == 0 || d[0] != '{' {
		t.Fatalf("dict=%q", d)
	}
	if !bytesContains(d, []byte("NaN")) {
		t.Fatalf("expected NaN token in expanded JSON dict: %q", d)
	}
}

func TestMutatorDictForTargetXML(t *testing.T) {
	d := MutatorDictForTarget("expat")
	if len(d) == 0 || d[0] != '<' {
		t.Fatalf("dict=%q", d)
	}
	if !bytesContains(d, []byte("&quot;")) {
		t.Fatalf("expected &quot; in expanded XML dict: %q", d)
	}
}

func TestMutatorDictForTargetMsgpack(t *testing.T) {
	d := MutatorDictForTarget("lib_msgpack_codec")
	if len(d) == 0 || d[0] != 0x80 {
		t.Fatalf("dict=%v", d)
	}
	if !bytesContains(d, []byte{0xc0}) {
		t.Fatalf("expected msgpack nil (0xc0) in expanded dict: %v", d)
	}
}

func TestMutatorDictGenericFallback(t *testing.T) {
	d := MutatorDictForTarget("libpng")
	if len(d) == 0 {
		t.Fatal("unknown catalog target should still get a generic dict")
	}
	text := LibFuzzerDictTextForTarget("libpng")
	if !strings.Contains(text, "\"%n\"") || !strings.Contains(text, "\"2147483648\"") {
		t.Fatalf("generic lf dict=%q", text)
	}
}

func TestLibFuzzerDictTextProfiles(t *testing.T) {
	js := LibFuzzerDictTextForTarget("jsmn")
	if !strings.Contains(js, "\"null\"") || !strings.Contains(js, "\"NaN\"") {
		t.Fatalf("json dict=%q", js)
	}
	msg := LibFuzzerDictTextForTarget("lib_msgpack_codec")
	if !strings.Contains(msg, `\x80`) || !strings.Contains(msg, `\xc0`) {
		t.Fatalf("msgpack dict=%q", msg)
	}
	if LibFuzzerDictTextForTarget("") != "" {
		t.Fatal("empty target must not invent a dict")
	}
}

func TestEnsureLibFuzzerDictKeepsPackDict(t *testing.T) {
	cfg := map[string]any{"libfuzzer_dict": "hw_magic=\"HWIR\"\n"}
	EnsureLibFuzzerDict(cfg, "jsmn")
	if cfg["libfuzzer_dict"] != "hw_magic=\"HWIR\"\n" {
		t.Fatalf("pack dict overwritten: %#v", cfg["libfuzzer_dict"])
	}
	empty := map[string]any{}
	EnsureLibFuzzerDict(empty, "cjson")
	s, _ := empty["libfuzzer_dict"].(string)
	if !strings.Contains(s, "\"null\"") {
		t.Fatalf("catalog dict=%q", s)
	}
}

func TestApplyHuntMutatorDict(t *testing.T) {
	cfg := map[string]any{}
	ApplyHuntMutatorDict(cfg, "jsmn")
	if cfg["mutator_dict"] == nil {
		t.Fatal("expected mutator_dict")
	}
	if cfg["hunt_mutator_profile"] != "json" {
		t.Fatalf("profile=%v", cfg["hunt_mutator_profile"])
	}
}

func bytesContains(haystack, needle []byte) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		ok := true
		for j := 0; j < len(needle); j++ {
			if haystack[i+j] != needle[j] {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
