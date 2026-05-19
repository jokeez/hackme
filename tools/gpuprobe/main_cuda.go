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
	a := accs[0]
	const trials = 1 << 22
	mod := uint64(1_000_003)
	t0 := time.Now()
	found, nonce, err := a.Search(context.Background(), 1, trials, mod)
	elapsed := time.Since(t0)
	_ = a.Close()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Search error: %v\n", err)
		os.Exit(3)
	}
	ghs := float64(trials) / elapsed.Seconds() / 1e9
	fmt.Printf("Smoke: found=%v nonce=%d elapsed=%s ~%.2f GH/s\n", found, nonce, elapsed.Round(time.Millisecond), ghs)
	fmt.Println("CUDA probe OK")
}
