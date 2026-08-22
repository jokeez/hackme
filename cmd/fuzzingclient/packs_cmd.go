package main

import (
	"encoding/json"
	"fmt"
	"os"

	"hackme/internal/fuzzingcli"
)

func doPacks(args []string) error {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Fprintln(os.Stderr, "usage: hackme-fuzzing packs")
		fmt.Fprintln(os.Stderr, "  Lists ready detector packs (no custom WASM rule required).")
		fmt.Fprintln(os.Stderr, "  Then: hackme-fuzzing wizard --pack secrets --package audit")
		return nil
	}
	list := fuzzingcli.ListGuardPacks()
	out := make([]map[string]any, 0, len(list))
	for _, p := range list {
		sample := ""
		if len(p.SeedByteCorpus) > 0 {
			if s, ok := p.SeedByteCorpus[0].(string); ok {
				sample = s
				if p.ID == "filter_utf8" {
					sample = `\xc7=`
				}
			}
		}
		out = append(out, map[string]any{
			"id":              p.ID,
			"title":           p.Title,
			"summary":         p.Summary,
			"input_mode":      p.InputMode,
			"default_package": p.DefaultPackage,
			"budget_presets": map[string]any{
				"scan":  map[string]any{"runs": p.ScanRuns, "seconds": p.ScanSeconds},
				"audit": map[string]any{"runs": p.AuditRuns, "seconds": p.AuditSeconds},
				"deep":  map[string]any{"runs": p.DeepRuns, "seconds": p.DeepSeconds},
			},
			"explain_sample": fuzzingcli.ExplainPackFinding(p.ID, sample, ""),
			"wizard":         fmt.Sprintf("hackme-fuzzing wizard --pack %s --package %s", p.ID, p.DefaultPackage),
		})
	}
	b, _ := json.MarshalIndent(map[string]any{"ok": true, "packs": out}, "", "  ")
	fmt.Println(string(b))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Ready packs (choose one — no custom rule):")
	for _, p := range list {
		fmt.Fprintf(os.Stderr, "  %-14s  %s\n", p.ID, p.Title)
		fmt.Fprintf(os.Stderr, "                 %s\n", p.Summary)
		fmt.Fprintf(os.Stderr, "                 → hackme-fuzzing wizard --pack %s\n\n", p.ID)
	}
	return nil
}
