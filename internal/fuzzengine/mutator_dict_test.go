package fuzzengine

import (
	"encoding/json"
	"testing"
)

func TestParseMutatorDictHex(t *testing.T) {
	d := ParseMutatorDict(map[string]any{"mutator_dict": "414243"})
	if string(d) != "ABC" {
		t.Fatalf("got %q", d)
	}
}

func TestParseMutatorDictJSONByteRoundTrip(t *testing.T) {
	orig := []byte("AKIAASIAghp_github_pat")
	raw, err := json.Marshal(map[string]any{"mutator_dict": orig})
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	got := ParseMutatorDict(cfg)
	if string(got) != string(orig) {
		t.Fatalf("json []byte round-trip corrupted dict: got %q want %q (wire=%s)", got, orig, raw)
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
