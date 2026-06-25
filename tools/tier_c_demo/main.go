// Command tier_c_demo runs Tier-C ASAN repro on the intentional demo harness.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"hackme/internal/fuzznative"
)

func main() {
	guard := flag.String("guard", "demo_stack_overflow", "harness guard name")
	outDir := flag.String("out", ".", "report output directory")
	flag.Parse()

	root := os.Getenv("HACKME_REPO_ROOT")
	if root == "" {
		root, _ = os.Getwd()
	}
	pins, _ := fuzznative.LoadPins(root)

	cases := []struct {
		name  string
		input []byte
	}{
		{"clean_len_4", []byte{0x04, 0x41, 0x42, 0x43, 0x44, 0, 0, 0}},
		{"crash_len_16", []byte{0x10, 0x41, 0x42, 0x43, 0x44, 0x45, 0x46, 0x47}},
		{"crash_len_32", []byte{0x20, 0xff, 0x00, 0xaa, 0x55, 0x11, 0x22, 0x33}},
	}

	report := map[string]any{
		"guard":        *guard,
		"repro_mode":   "asan_binary",
		"disclaimer":   "intentional demo vuln — not a real CVE in upstream software",
		"cases":        []map[string]any{},
		"cve_pipeline": false,
	}

	var crashFound bool
	for _, tc := range cases {
		res := fuzznative.EvalReproEx(fuzznative.ReproModeAsanBinary, "demo", *guard, tc.input, pins, root)
		entry := map[string]any{
			"name":      tc.name,
			"input_hex": fmt.Sprintf("%x", tc.input),
			"status":    res.Status,
			"note":      res.Note,
		}
		if res.Status == fuzznative.StatusNativeCrash {
			crashFound = true
			entry["cve_class"] = "CWE-121 stack-buffer-overflow (demo)"
		}
		report["cases"] = append(report["cases"].([]map[string]any), entry)
		fmt.Printf("[%s] status=%s input=%x\n", tc.name, res.Status, tc.input)
		if res.Note != "" {
			fmt.Printf("  note: %s\n", trunc(res.Note, 200))
		}
	}
	report["cve_pipeline"] = crashFound

	b, _ := json.MarshalIndent(report, "", "  ")
	outPath := filepath.Join(*outDir, "CVE_DEMO_REPORT.json")
	_ = os.WriteFile(outPath, append(b, '\n'), 0o644)
	if crashFound {
		fmt.Println("\n[cve-demo] PASS — native_crash detected (Tier-C CVE pipeline works)")
	} else {
		fmt.Println("\n[cve-demo] FAIL — expected native_crash on overflow inputs")
		os.Exit(1)
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
