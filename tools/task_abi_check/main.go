package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hackme/internal/sandbox"
)

type checkResult struct {
	Path      string `json:"path"`
	Bytes     int    `json:"bytes"`
	Valid     bool   `json:"valid"`
	Error     string `json:"error,omitempty"`
	CheckedAt string `json:"checked_at"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./tools/task_abi_check <wasm-file> [more.wasm ...]")
		os.Exit(2)
	}

	results := make([]checkResult, 0, len(os.Args)-1)
	failed := false
	for _, p := range os.Args[1:] {
		abs, _ := filepath.Abs(p)
		r := checkResult{
			Path:      abs,
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			r.Valid = false
			r.Error = err.Error()
			results = append(results, r)
			failed = true
			continue
		}
		r.Bytes = len(raw)
		if err := sandbox.ValidateCheckWasm(context.Background(), raw); err != nil {
			r.Valid = false
			r.Error = err.Error()
			results = append(results, r)
			failed = true
			continue
		}
		r.Valid = true
		results = append(results, r)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{
		"ok":      !failed,
		"results": results,
	})
	if failed {
		os.Exit(1)
	}
}
