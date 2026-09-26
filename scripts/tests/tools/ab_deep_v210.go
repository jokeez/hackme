//go:build ignore

package main

import (
	"fmt"

	"hackme/internal/fuzzengine"
)

func main() {
	base := []byte(`{"a":1,"nested":{"b":[1,2,3],"tag":"<x/>"}}`)
	dict := []byte(`"null""true""false""a""nested"{}[]`)
	corpus := [][]byte{[]byte(`{}`), []byte(`[1,2]`), []byte(`{"x":null}`), []byte(`<root/>`), base}
	cfg28 := map[string]any{"havoc_deep_v28": true}
	cfg210 := map[string]any{"havoc_deep_v28": true, "havoc_deep_v210": true}
	for _, n := range []int{5000, 20000} {
		s28 := fuzzengine.MeasureMutationDepthWithCFG(base, dict, corpus, n, 256, cfg28)
		s210 := fuzzengine.MeasureMutationDepthWithCFG(base, dict, corpus, n, 256, cfg210)
		ug, lg := 0.0, 0.0
		if s28.UniqueSHA256 > 0 {
			ug = 100 * float64(s210.UniqueSHA256-s28.UniqueSHA256) / float64(s28.UniqueSHA256)
		}
		if s28.UniqueLens > 0 {
			lg = 100 * float64(s210.UniqueLens-s28.UniqueLens) / float64(s28.UniqueLens)
		}
		fmt.Printf("n=%d v28_u=%d v210_u=%d gain_u=%.2f%% v28_lens=%d v210_lens=%d gain_lens=%.2f%%\n",
			n, s28.UniqueSHA256, s210.UniqueSHA256, ug, s28.UniqueLens, s210.UniqueLens, lg)
	}
	fmt.Println("engine", fuzzengine.Version)
}
