package hunt

import "testing"

func TestMutatorDictForTargetJSON(t *testing.T) {
	d := MutatorDictForTarget("cjson")
	if len(d) == 0 || d[0] != '{' {
		t.Fatalf("dict=%q", d)
	}
}

func TestMutatorDictForTargetXML(t *testing.T) {
	d := MutatorDictForTarget("expat")
	if len(d) == 0 || d[0] != '<' {
		t.Fatalf("dict=%q", d)
	}
}

func TestMutatorDictForTargetMsgpack(t *testing.T) {
	d := MutatorDictForTarget("lib_msgpack_codec")
	if len(d) == 0 || d[0] != 0x80 {
		t.Fatalf("dict=%v", d)
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
