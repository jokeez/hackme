//go:build ignore

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"

	"hackme/internal/sandbox"
)

func main() {
	ctx := context.Background()
	u64Wasm, _ := os.ReadFile("tasks/artifacts/security/rust_tracefuse_detector_guard.wasm")
	bytesWasm, _ := os.ReadFile("tasks/artifacts/security/rust_tracefuse_detector_bytes_guard.wasm")
	lines := []string{
		"AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
		"GITHUB_PAT=ghp_FAKEEXAMPLETOKENX1234567890123456789",
		"FROM node:latest",
		"ENV API_SECRET=FAKE_EXAMPLE_DOCKER_SECRET_DO_NOT_USE",
		"hello benign config",
	}
	fmt.Println("=== 8B u64 guard (prod-style) vs check_bytes guard (local P4) ===\n")
	for _, line := range lines {
		u64 := packLE8(line)
		u64Hit, _ := sandbox.InvokeCheck(ctx, u64Wasm, u64)
		bytesHit, _ := sandbox.InvokeCheckInput(ctx, bytesWasm, []byte(line))
		fmt.Printf("LINE (%2d B): %s\n", len(line), trunc(line, 55))
		fmt.Printf("  u64 window %q  -> hit=%v\n", trunc(string(packBytes(u64)), 12), u64Hit)
		fmt.Printf("  check_bytes full -> hit=%v\n\n", bytesHit)
	}
}

func packLE8(s string) uint64 {
	b := append([]byte(s), make([]byte, 8)...)[:8]
	return binary.LittleEndian.Uint64(b)
}

func packBytes(u uint64) []byte {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], u)
	return b[:]
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
