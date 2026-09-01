// Hunt local benchmark helper (Standard package defaults).
//go:build ignore

package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/hunt"
)

func main() {
	target := flag.String("target", "cjson", "catalog target id")
	pkg := flag.String("package", "hunt_standard", "hunt package key")
	iter := flag.Int("iter", 15000, "iteration budget")
	wall := flag.Int("wall", 120, "wall seconds")
	out := flag.String("out", "", "output json path")
	crashesDir := flag.String("crashes-dir", "", "write crash inputs as .bin + index.json")
	reportPath := flag.String("report", "", "full HuntReport json (crashes included)")
	flag.Parse()

	cfg := map[string]any{
		"upstream_target_id": *target,
		"hunt_package":       *pkg,
	}
	hunt.ApplyPackageDepthDefaults(cfg, *pkg, false)
	hunt.ApplyHuntMutatorDict(cfg, *target)
	hunt.ApplySanitizerDefaults(cfg, *pkg)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*wall)*time.Second)
	defer cancel()

	start := time.Now()
	rep, err := hunt.LocalRunWithConfig(ctx, hunt.LocalRunOptions{
		RepoRoot:         hunt.RepoRoot(),
		TargetID:         *target,
		BudgetIterations: *iter,
		TimeLimitSec:     *wall,
		Config:           cfg,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "hunt local: %v\n", err)
		os.Exit(1)
	}
	elapsed := time.Since(start).Seconds()
	eps := 0.0
	if elapsed > 0 {
		eps = float64(rep.Iterations) / elapsed
	}
	bySub := map[string]int{}
	for _, c := range rep.Crashes {
		key := c.SanitizerClass + "/" + c.SanitizerSubtype
		bySub[key]++
	}
	bySig := map[string]int{}
	byStack := map[string]int{}
	for _, c := range rep.Crashes {
		sig := c.SanitizerClass + "/" + c.SanitizerSubtype
		bySig[sig]++
		stackKey := ""
		for _, line := range strings.Split(c.Sanitizer, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, ".c:") || strings.Contains(line, ".cpp:") {
				stackKey = line
				break
			}
		}
		if stackKey == "" {
			stackKey = "(no source frame)"
		}
		byStack[stackKey]++
	}

	result := map[string]any{
		"engine":               "hunt_local",
		"target":               *target,
		"hunt_package":         *pkg,
		"iterations_per_shard": hunt.IterationsPerShardForPackage(*pkg),
		"mutator_profile":      cfg["hunt_mutator_profile"],
		"verdict":              rep.Verdict,
		"iterations":           rep.Iterations,
		"crashes":              len(rep.Crashes),
		"unique_inputs":        len(rep.Crashes),
		"unique_signatures":    len(bySig),
		"unique_stack_frames":  len(byStack),
		"elapsed_sec":          elapsed,
		"exec_per_sec":         eps,
		"sanitizer_subtypes":   bySub,
		"sanitizer_signatures": bySig,
		"stack_frames":         byStack,
		"hunt_detect_leaks":    cfg["hunt_detect_leaks"],
		"local_budget_iters":   cfg["hunt_local_budget_iterations"],
	}
	if *crashesDir != "" {
		_ = os.MkdirAll(*crashesDir, 0o755)
		index := make([]map[string]any, 0, len(rep.Crashes))
		for i, c := range rep.Crashes {
			raw, err := hex.DecodeString(c.InputHex)
			if err != nil {
				continue
			}
			name := fmt.Sprintf("crash-%04d-%s.bin", i+1, c.SanitizerSubtype)
			_ = os.WriteFile(filepath.Join(*crashesDir, name), raw, 0o644)
			index = append(index, map[string]any{
				"file":              name,
				"len":               c.InputLen,
				"trimmed":           c.Trimmed,
				"original_len":      c.OriginalInputLen,
				"sanitizer_class":   c.SanitizerClass,
				"sanitizer_subtype": c.SanitizerSubtype,
				"iteration":         c.Iteration,
			})
		}
		b, _ := json.MarshalIndent(index, "", "  ")
		_ = os.WriteFile(filepath.Join(*crashesDir, "index.json"), b, 0o644)
	}
	if *reportPath != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		_ = os.WriteFile(*reportPath, b, 0o644)
	}
	if *out != "" {
		b, _ := json.MarshalIndent(result, "", "  ")
		_ = os.WriteFile(*out, b, 0o644)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}
