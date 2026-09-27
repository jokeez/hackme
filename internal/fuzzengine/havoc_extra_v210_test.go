package fuzzengine

import "testing"

func TestDeepHavocV210BurstChangesOutput(t *testing.T) {
	base := []byte(`{"hello":"world","n":42}`)
	dict := []byte(`"null""true"{}[]`)
	cfg28 := map[string]any{"havoc_deep_v28": true}
	cfg210 := map[string]any{"havoc_deep_v28": true, "havoc_deep_v210": true}
	same, diff := 0, 0
	for i := 0; i < 200; i++ {
		stage := MutationStage(StageHavocBase + (i % 32))
		salt := uint64(i)*0x9E3779B97F4A7C15 + 99
		a := MutateBytesForHunt(base, stage, salt, 256, cfg28, nil)
		b := MutateBytesForHunt(base, stage, salt, 256, cfg210, [][]byte{[]byte(`other`), dict})
		if string(a) == string(b) {
			same++
		} else {
			diff++
		}
	}
	if diff < 100 {
		t.Fatalf("v210 should rewrite many samples vs v28-only: diff=%d same=%d", diff, same)
	}
}

func TestDeepHavocV210LensGain(t *testing.T) {
	base := []byte(`{"a":1,"nested":{"b":[1,2,3]}}`)
	dict := []byte(`"null""true""false"{}[]`)
	corpus := [][]byte{[]byte(`{}`), []byte(`[1,2]`), base}
	cfg := map[string]any{"havoc_deep_v28": true, "havoc_deep_v210": true}
	st := MeasureMutationDepthWithCFG(base, dict, corpus, 3000, 256, cfg)
	if st.UniqueLens < 200 {
		t.Fatalf("v210 lens diversity too low: %+v", st)
	}
	if st.UniqueRatio < 0.9 {
		t.Fatalf("v210 unique ratio too low: %+v", st)
	}
	t.Logf("v210 depth: %+v", st)
}

func TestDeepHavocV210ImpliesV28Stack(t *testing.T) {
	base := []byte(`hello-world-input`)
	stage := MutationStage(StageHavocBase + 9)
	salt := uint64(777)
	only210 := map[string]any{"havoc_deep_v210": true}
	explicit := map[string]any{"havoc_deep_v28": true, "havoc_deep_v210": true}
	a := MutateBytesForHunt(base, stage, salt, 256, only210, nil)
	b := MutateBytesForHunt(base, stage, salt, 256, explicit, nil)
	if string(a) != string(b) {
		t.Fatal("havoc_deep_v210 alone must apply the same v2.8+burst pipeline as explicit flags")
	}
	core := MutateBytesForHunt(base, stage, salt, 256, nil, nil)
	if string(a) == string(core) {
		t.Fatal("v2.10 opt-in must not collapse to core mutations")
	}
}

func TestDeepHavocV210ZeroMaxLen(t *testing.T) {
	base := []byte(`abc`)
	cfg := map[string]any{"havoc_deep_v210": true}
	out := MutateBytesForHunt(base, StageHavocBase, 1, 0, cfg, nil)
	if len(out) == 0 {
		t.Fatal("maxLen=0 must normalize; burst must not empty/panic")
	}
	if len(out) > DefaultMaxInputBytesStd {
		t.Fatalf("capped length exceeded: %d", len(out))
	}
}
