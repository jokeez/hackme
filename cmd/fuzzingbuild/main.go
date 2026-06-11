// Command fuzzingbuild (hackme-fuzzing-build) — local WASM compile + manifest for fuzzing orders.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"hackme/internal/taskbuild"
)

func main() {
	lang := flag.String("lang", "rust", "language: rust, c, cpp, wat, tinygo, zig, assemblyscript")
	source := flag.String("source", "", "source file path")
	out := flag.String("out", "fuzzing-out", "output directory")
	id := flag.String("id", "", "order id")
	reward := flag.Float64("reward", 0.01, "reward_hmc per solve")
	diff := flag.Int("difficulty", 5, "difficulty_score")
	target := flag.Int("target", 3, "target_solves")
	payer := flag.String("payer-ref", "", "payer_ref")
	flag.Parse()
	if *source == "" {
		fmt.Fprintln(os.Stderr, "error: -source file required")
		fmt.Fprintln(os.Stderr, "usage: hackme-fuzzing-build -lang rust -source check.rs [-out dir] [-id name]")
		os.Exit(2)
	}
	code, err := os.ReadFile(*source)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := taskbuild.BuildFromSource(ctx, taskbuild.Options{
		ID:              *id,
		Language:        *lang,
		Source:          string(code),
		RewardHMC:       *reward,
		DifficultyScore: *diff,
		TargetSolves:    *target,
		PayerRef:        *payer,
		OutDir:          *out,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "wasm: %s (%d bytes) sha256=%s\n", res.WasmPath, len(res.WasmBytes), res.ArtifactHash)
	fmt.Println(string(res.ManifestJSON))
}
