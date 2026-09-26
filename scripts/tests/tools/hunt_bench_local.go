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

	"hackme/internal/fuzzengine"
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
	bySig := map[string]int{}
	byStack := map[string]int{}
	byFamily := map[string]int{}
	for _, c := range rep.Crashes {
		key := c.SanitizerClass + "/" + c.SanitizerSubtype
		bySub[key]++
		bySig[key]++
		fam := strings.TrimSpace(c.SanitizerClass) + "/" + strings.TrimSpace(c.SanitizerSubtype)
		if c.SanitizerClass == "" || c.SanitizerSubtype == "" {
			fam = fuzzengine.FindingFamily(c.SanitizerClass, c.Sanitizer)
		}
		byFamily[fam]++
		stackKey := ""
		for _, line := range strings.Split(c.Sanitizer, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			// Prefer source frames; UBSan often only prints "runtime error: …".
			if strings.Contains(line, ".c:") || strings.Contains(line, ".cpp:") ||
				strings.Contains(line, ".cc:") || strings.Contains(line, ".h:") ||
				strings.Contains(line, ".hpp:") || strings.Contains(line, ".rs:") {
				stackKey = line
				break
			}
			if stackKey == "" && (strings.Contains(line, "runtime error:") ||
				strings.Contains(line, "SUMMARY:") || strings.HasPrefix(line, "#0 ")) {
				stackKey = line
			}
		}
		if stackKey == "" {
			// Fall back to stable finding family (class+message) — never bucket everything as one.
			stackKey = fam
		}
		byStack[stackKey]++
	}

	rawInputs := len(rep.Crashes)
	familyCount := len(byFamily)
	if familyCount == 0 {
		familyCount = len(bySig)
	}
	collapse := 0.0
	if rawInputs > 0 && familyCount > 0 {
		collapse = 1.0 - float64(familyCount)/float64(rawInputs)
	}
	topFamilies := make([]map[string]any, 0, 8)
	type pair struct {
		k string
		n int
	}
	pairs := make([]pair, 0, len(byFamily))
	for k, n := range byFamily {
		pairs = append(pairs, pair{k: k, n: n})
	}
	for i := 1; i < len(pairs); i++ {
		j := i
		for j > 0 && (pairs[j].n > pairs[j-1].n || (pairs[j].n == pairs[j-1].n && pairs[j].k < pairs[j-1].k)) {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
			j--
		}
	}
	for i := 0; i < len(pairs) && i < 8; i++ {
		topFamilies = append(topFamilies, map[string]any{"family": pairs[i].k, "inputs": pairs[i].n})
	}
	findingFamilies := map[string]any{
		"family_count":    familyCount,
		"raw_input_count": rawInputs,
		"crash_inputs":    rawInputs,
		"hygiene_inputs":  0,
		"collapse_ratio":  collapse,
		"by_family":       byFamily,
		"top_families":    topFamilies,
		"honesty_note":    "Cite family_count, not raw_input_count — many inputs often share one root cause.",
	}
	diversity := 0.0
	if rawInputs > 0 && familyCount > 0 {
		diversity = float64(familyCount) / float64(rawInputs)
	}
	rare, hot := 0, 0
	for _, n := range byFamily {
		if n <= 2 {
			rare++
		}
		if n >= 8 {
			hot++
		}
	}
	corpusHealth := map[string]any{
		"ok":                  true,
		"source":              "hunt_local_soak",
		"seed_count":          rawInputs,
		"rare_family_seeds":   rare,
		"hot_family_seeds":    hot,
		"unique_signatures":   len(bySig),
		"unique_stack_frames": len(byStack),
		"diversity":           diversity,
		"iterations":          rep.Iterations,
		"note":                "Local soak proxy — cite finding_families; not fleet pool_corpus rarity.",
	}

	result := map[string]any{
		"engine":               "hunt_local",
		"target":               *target,
		"language":             rep.Language,
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
		"finding_families":     findingFamilies,
		"corpus_health":        corpusHealth,
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
