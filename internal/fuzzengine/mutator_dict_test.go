package fuzzengine

import "testing"

func TestParseMutatorDictHex(t *testing.T) {
	d := ParseMutatorDict(map[string]any{"mutator_dict": "414243"})
	if string(d) != "ABC" {
		t.Fatalf("got %q", d)
	}
}

func TestMutateBytesPackDict(t *testing.T) {
	cfg := map[string]any{"mutator_dict": []byte("AKIA")}
	base := []byte("xxxxxxxx")
	a := MutateBytesForConfig(base, StageHavocBase+3, 99, 64, cfg)
	b := MutateBytesForConfig(base, StageHavocBase+3, 99, 64, cfg)
	if string(a) != string(b) {
		t.Fatal("not deterministic")
	}
}

func TestPowerMutCapByTier(t *testing.T) {
	if PowerMutCap(ApplyDepthTier(nil, DepthWasmOnly)) != 2 {
		t.Fatal("scan cap")
	}
	if PowerMutCap(ApplyDepthTier(nil, DepthWasmNative)) != 6 {
		t.Fatal("audit cap")
	}
	if PowerMutCap(ApplyDepthTier(nil, DepthBytesCorpus)) != 12 {
		t.Fatal("deep cap")
	}
}
