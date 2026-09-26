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
