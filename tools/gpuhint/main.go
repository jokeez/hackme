package main

import (
	"fmt"
	"os"

	"hackme/internal/gputune"
)

func main() {
	name := "Unknown GPU"
	if len(os.Args) > 1 {
		name = os.Args[1]
	}
	h := gputune.ForGPUName(name)
	fmt.Printf("%s | %s | PL=%dW (range %d-%d)\n", h.Vendor, h.Family, h.RecommendedPL, h.PLRangeMin, h.PLRangeMax)
}
