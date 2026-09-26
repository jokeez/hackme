//go:build ignore

package main

import (
	"fmt"

	"hackme/internal/fuzzengine"
)

func main() {
	base := []byte(`{"a":1,"nested":{"b":[1,2,3],"tag":"<x/>"}}`)
	dict := []byte(`"null""true""false""a""nested"{}[]`)
	corpus := [][]byte{
		[]byte(`{}`), []byte(`[1,2]`), []byte(`{"x":null}`),
		[]byte(`<root><child/></root>`), base,
	}
	for _, n := range []int{5000, 50000} {
		rep := fuzzengine.CompareEngineAB(base, dict, corpus, n, 256)
		fmt.Printf("n=%d gain_unique=%.2f%% gain_lens=%.2f%% base_u=%d cur_u=%d base_lens=%d cur_lens=%d\n",
			n, rep.UniqueGainPct, rep.LensGainPct,
			rep.Baseline.UniqueSHA256, rep.Current.UniqueSHA256,
			rep.Baseline.UniqueLens, rep.Current.UniqueLens)
	}
}
