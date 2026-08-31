//go:build ignore

package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"hackme/internal/hunt"
)

func main() {
	cfg := map[string]any{
		"upstream_target_id": "libucl",
		"hunt_package":       "hunt_standard",
	}
	hunt.ApplyPackageDepthDefaults(cfg, "hunt_standard", false)
	hunt.ApplyHuntMutatorDict(cfg, "libucl")
	hunt.ApplySanitizerDefaults(cfg, "hunt_standard")

	wall := 45
	if s := strings.TrimSpace(os.Getenv("WALL_SEC")); s != "" {
		fmt.Sscanf(s, "%d", &wall)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(wall)*time.Second)
	defer cancel()
	rep, err := hunt.LocalRunWithConfig(ctx, hunt.LocalRunOptions{
		RepoRoot:         hunt.RepoRoot(),
		TargetID:         "libucl",
		BudgetIterations: 4000,
		TimeLimitSec:     wall,
		Config:           cfg,
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("iterations=%d crashes=%d verdict=%s\n\n", rep.Iterations, len(rep.Crashes), rep.Verdict)

	bySubtype := map[string]int{}
	bySig := map[string]int{}
	byLen := map[int]int{}
	byStack := map[string]int{}
	for _, c := range rep.Crashes {
		st := c.SanitizerClass + "/" + c.SanitizerSubtype
		bySubtype[st]++
		sig := st
		stackKey := ""
		for _, line := range strings.Split(c.Sanitizer, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "runtime error:") {
				sig += " | " + line
			}
			if strings.Contains(line, ".c:") && strings.Contains(line, "ucl_") {
				stackKey = line
			}
		}
		if stackKey == "" {
			stackKey = "(no ucl frame)"
		}
		byStack[stackKey]++
		bySig[sig]++
		byLen[c.InputLen]++
	}

	fmt.Printf("unique_inputs=%d (dedup key = full mutated input bytes)\n", len(rep.Crashes))
	fmt.Printf("unique_runtime_error_lines=%d\n", len(bySig))
	fmt.Printf("unique_stack_frames=%d\n\n", len(byStack))

	fmt.Println("by_subtype:")
	for k, v := range bySubtype {
		fmt.Printf("  %s: %d\n", k, v)
	}
	fmt.Println("\nby_input_len:")
	for k, v := range byLen {
		fmt.Printf("  len=%d: %d\n", k, v)
	}
	fmt.Println("\nsanitizer_signatures:")
	for k, v := range bySig {
		fmt.Printf("  [%d] %s\n", v, k)
	}
	fmt.Println("\nstack_frames:")
	for k, v := range byStack {
		fmt.Printf("  [%d] %s\n", v, k)
	}
	// known minimal repro from disclosure
	known := `{"a":1}{"a":1}`
	fmt.Printf("\nknown_repro %q in set: ", known)
	foundKnown := false
	for _, c := range rep.Crashes {
		raw, _ := hex.DecodeString(c.InputHex)
		if string(raw) == known {
			foundKnown = true
			break
		}
	}
	fmt.Println(foundKnown)
	if len(rep.Crashes) > 0 {
		fmt.Println("\nfirst crash sanitizer excerpt:")
		lines := strings.Split(rep.Crashes[0].Sanitizer, "\n")
		for i, line := range lines {
			if i > 12 {
				break
			}
			fmt.Println(" ", line)
		}
		fmt.Println("\nfirst 8 samples (len, subtype, ascii):")
		for i := 0; i < len(rep.Crashes) && i < 8; i++ {
			c := rep.Crashes[i]
			raw, _ := hex.DecodeString(c.InputHex)
			fmt.Printf("  #%d len=%d %s ascii=%q\n", i+1, c.InputLen, c.SanitizerSubtype, printable(raw))
		}
	}
}

func printable(b []byte) string {
	if len(b) > 48 {
		b = b[:48]
	}
	out := make([]byte, len(b))
	for i, c := range b {
		if c >= 32 && c < 127 {
			out[i] = c
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}
