// Hunt local benchmark helper (Standard package defaults).
//go:build ignore

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"hackme/internal/hunt"
)

func main() {
	target := flag.String("target", "cjson", "catalog target id")
	pkg := flag.String("package", "hunt_standard", "hunt package key")
	iter := flag.Int("iter", 15000, "iteration budget")
	wall := flag.Int("wall", 120, "wall seconds")
	out := flag.String("out", "", "output json path")
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
	result := map[string]any{
		"engine":               "hunt_local",
		"target":               *target,
		"hunt_package":         *pkg,
		"iterations_per_shard": hunt.IterationsPerShardForPackage(*pkg),
		"mutator_profile":      cfg["hunt_mutator_profile"],
		"verdict":              rep.Verdict,
		"iterations":           rep.Iterations,
		"crashes":              len(rep.Crashes),
		"elapsed_sec":          elapsed,
		"exec_per_sec":         eps,
		"sanitizer_subtypes":   bySub,
		"hunt_detect_leaks":    cfg["hunt_detect_leaks"],
		"local_budget_iters":   cfg["hunt_local_budget_iterations"],
	}
	if *out != "" {
		b, _ := json.MarshalIndent(result, "", "  ")
		_ = os.WriteFile(*out, b, 0o644)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
}
