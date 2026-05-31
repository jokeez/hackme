// Golden test: upstream_hackme_order_gate.wasm matches Go InsertOrderTask floors.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"hackme/internal/sandbox"
)

func goReject(n uint64) bool {
	rewardMilli := int(n & 0xffff)
	diff := int((n >> 16) & 0xff)
	target := int((n >> 24) & 0xffff)

	if target < 1 || target > 10000 {
		return true
	}
	if diff < 1 || diff > 100 {
		return true
	}
	reward := float64(rewardMilli) / 1000.0
	minReward := float64(diff) * 0.0005
	if reward+1e-12 < minReward {
		return true
	}
	prepaid := reward * float64(target)
	if prepaid+1e-12 < 0.05 {
		return true
	}
	return false
}

func main() {
	root := findRoot()
	wasmPath := filepath.Join(root, "tasks/artifacts/security/upstream_hackme_order_gate.wasm")
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build wasm first: bash scripts/build_upstream_l1_pack.sh\n")
		os.Exit(1)
	}
	ctx := context.Background()
	if err := sandbox.ValidateCheckWasm(ctx, raw); err != nil {
		panic(err)
	}
	const rounds = 20000
	mismatch := 0
	for i := 0; i < rounds; i++ {
		n := uint64(i*7919+1) ^ uint64(i<<17) ^ uint64(i%997)*0x10001
		want := goReject(n)
		ok, err := sandbox.InvokeCheck(ctx, raw, n)
		if err != nil {
			fmt.Printf("trap at i=%d n=%x err=%v\n", i, n, err)
			os.Exit(1)
		}
		got := ok // wasm 1 = reject
		if got != want {
			mismatch++
			if mismatch <= 5 {
				fmt.Printf("mismatch i=%d n=%x go=%v wasm=%v\n", i, n, want, got)
			}
		}
	}
	if mismatch > 0 {
		fmt.Printf("FAIL mismatches=%d/%d\n", mismatch, rounds)
		os.Exit(1)
	}
	fmt.Printf("PASS hackme_order_gate golden %d inputs\n", rounds)
}

func findRoot() string {
	cwd, _ := os.Getwd()
	for d := cwd; d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
	}
	return cwd
}
