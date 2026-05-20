//go:build cuda && !opencl

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"hackme/internal/gpupoh"
)

func main() {
	fmt.Println("HackMe GPU probe (CUDA build)")
	if os.Getenv("HACKME_CUDA_VERBOSE") == "" {
		_ = os.Setenv("HACKME_CUDA_VERBOSE", "1")
	}
	accs, err := gpupoh.DiscoverAccelerators()
	if err != nil {
		fmt.Fprintf(os.Stderr, "DiscoverAccelerators: %v\n", err)
		os.Exit(1)
	}
	if len(accs) == 0 {
		fmt.Println("No GPU accelerators found.")
		os.Exit(2)
	}
	for _, a := range accs {
		fmt.Printf("  %s — %s\n", a.Label(), a.DeviceName())
	}
	const trials = 1 << 22
	mod := uint64(1_000_003)
	var fail int
	for i, a := range accs {
		t0 := time.Now()
		found, nonce, err := a.Search(context.Background(), 1, trials, mod)
		elapsed := time.Since(t0)
		_ = a.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "device #%d Search error: %v\n", i, err)
			fail++
			continue
		}
		ghs := float64(trials) / elapsed.Seconds() / 1e9
		fmt.Printf("Smoke #%d: found=%v nonce=%d elapsed=%s ~%.2f GH/s (%s)\n",
			i, found, nonce, elapsed.Round(time.Millisecond), ghs, accs[i].Label())
	}
	if fail > 0 {
		fmt.Fprintf(os.Stderr, "CUDA probe: %d/%d devices failed smoke search\n", fail, len(accs))
		os.Exit(3)
	}
	fmt.Printf("CUDA probe OK (%d device(s))\n", len(accs))
}
