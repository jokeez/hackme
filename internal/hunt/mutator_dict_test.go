package hunt

import "testing"

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
