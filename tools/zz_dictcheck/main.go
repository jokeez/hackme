package main

import (
	"fmt"
	"os"
	"strings"

	"hackme/internal/hunt"
)

func main() {
	for _, id := range os.Args[1:] {
		dir := "/home/kapa/Desktop/HackMe/.cache/oss-cve-clones/" + id
		toks := hunt.DeriveDictTokensFromSources(dir, 0)
		fmt.Printf("%-12s tokens=%-4d  %s\n", id, len(toks), strings.Join(firstN(toks, 10), " "))
	}
}

func firstN(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
