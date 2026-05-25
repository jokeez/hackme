//go:build ignore

package main

import (
	"fmt"
	"os"
	"strconv"

	"hackme/internal/chain"
)

func main() {
	m, _ := strconv.ParseUint(os.Getenv("TARGET_MOD"), 10, 64)
	if m == 0 {
		m = 2_500_000
	}
	for x := uint64(0); x < 500_000_000; x++ {
		if chain.PohEval(x)%m == 0 {
			fmt.Println(x)
			return
		}
	}
	os.Exit(1)
}
