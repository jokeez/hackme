//go:build ignore

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"hackme/internal/hunt"
)

func main() {
	repo := flag.String("repo", ".", "repo root")
	target := flag.String("target", "", "target id")
	flag.Parse()
	cfg := map[string]any{}
	n, err := hunt.MergeLibFuzzerSeedCorpus(cfg, *repo, *target)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
		"merged": n,
		"guided": cfg["hunt_corpus_guided"] == true,
	})
}
