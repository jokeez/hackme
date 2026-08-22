// project_fuzz_matrix runs maximum local validation across guards and external CVE-class inputs.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

type caseResult struct {
	Name      string `json:"name"`
	Category  string `json:"category"`
	Input     string `json:"input_preview"`
	Hit       bool   `json:"hit"`
	PoolFound bool   `json:"pool_found,omitempty"`
	Note      string `json:"note,omitempty"`
}

type guardRow struct {
	Guard       string `json:"guard"`
	Mode        string `json:"mode"`
	Runs        int    `json:"runs"`
	Findings    int    `json:"findings"`
	KnownPOCHit bool   `json:"known_poc_hit"`
	Note        string `json:"note,omitempty"`
}

func main() {
	root := repoRoot()
	ctx := context.Background()
	var report struct {
		GeneratedAt string       `json:"generated_at"`
		Cases       []caseResult `json:"direct_cases"`
		Guards      []guardRow   `json:"guard_matrix"`
		External    []caseResult `json:"external"`
		Verdict     string       `json:"verdict"`
	}
	report.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	// --- CVE-class direct inputs ---
	cases := []struct {
		name, cat, input string
		guard            string
		bytes            bool
		u64              uint64
	}{
		{"fluxtap_poc", "cve_repro", "\xc7=", "rust_fluxtap_filter_bytes_guard.wasm", true, 0},
		{"script_push_521", "bitcoin_cve_class", "op=0x4c len=521", "rust_script_push_bounds_guard.wasm", false, 0x4c | (521 << 8)},
		{"tracefuse_akia", "supply_chain", "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE", "rust_tracefuse_detector_bytes_guard.wasm", true, 0},
		{"tracefuse_ghp", "supply_chain", "GITHUB_PAT=ghp_FAKEEXAMPLETOKENX1234567890123456789", "rust_tracefuse_detector_bytes_guard.wasm", true, 0},
		{"benign_dns", "negative", "dns", "rust_fluxtap_filter_bytes_guard.wasm", true, 0},
	}

	for _, c := range cases {
		wasmPath := filepath.Join(root, "tasks/artifacts/security", c.guard)
		raw, err := os.ReadFile(wasmPath)
		if err != nil {
			report.Cases = append(report.Cases, caseResult{Name: c.name, Category: c.cat, Note: "wasm missing: " + err.Error()})
			continue
		}
		var hit bool
		if c.bytes {
			hit, _ = sandbox.InvokeCheckInput(ctx, raw, []byte(c.input))
		} else {
			hit, _ = sandbox.InvokeCheck(ctx, raw, c.u64)
		}
		cr := caseResult{Name: c.name, Category: c.cat, Input: trunc(c.input, 60), Hit: hit}
		if c.name == "fluxtap_poc" && hit {
			cr.Note = "FluxTap filter.go panic class reproduced in WASM guard"
		}
		report.Cases = append(report.Cases, cr)
	}

	// --- Pool pilot on key guards ---
	guards := []struct {
		file string
		bytes bool
		poc  []byte
		u64  uint64
	}{
		{"rust_script_push_bounds_guard.wasm", false, nil, 0x4c | (521 << 8)},
		{"rust_fluxtap_filter_bytes_guard.wasm", true, []byte("\xc7="), 0},
		{"rust_tracefuse_detector_bytes_guard.wasm", true, []byte("AKIAIOSFODNN7EXAMPLE"), 0},
	}
	for _, g := range guards {
		wasmPath := filepath.Join(root, "tasks/artifacts/security", g.file)
		raw, err := os.ReadFile(wasmPath)
		if err != nil {
			report.Guards = append(report.Guards, guardRow{Guard: g.file, Note: err.Error()})
			continue
		}
		row, err := runGuardPool(ctx, hex.EncodeToString(raw), g.bytes, g.poc, g.u64, 128, 3)
		if err != nil {
			report.Guards = append(report.Guards, guardRow{Guard: g.file, Note: err.Error()})
			continue
		}
		row.Guard = g.file
		report.Guards = append(report.Guards, row)
	}

	// --- External: FluxTap native panic test ---
	ft := runFluxTapFilterTest(root)
	report.External = append(report.External, ft)

	// Verdict
	pocHits := 0
	pocTotal := 0
	for _, c := range report.Cases {
		if c.Category == "cve_repro" || c.Category == "bitcoin_cve_class" || c.Category == "supply_chain" {
			pocTotal++
			if c.Hit {
				pocHits++
			}
		}
	}
	switch {
	case pocHits == pocTotal && pocTotal > 0:
		report.Verdict = "READY_LOCAL"
	case pocHits >= pocTotal-1:
		report.Verdict = "NEAR_READY"
	default:
		report.Verdict = "GAPS"
	}

	outPath := filepath.Join(root, "tasks/artifacts/security/project_fuzz_matrix_report.json")
	b, _ := json.MarshalIndent(report, "", "  ")
	_ = os.WriteFile(outPath, b, 0o644)

	fmt.Println("===== Project Fuzz Matrix =====")
	fmt.Printf("direct POC hits: %d/%d\n", pocHits, pocTotal)
	for _, g := range report.Guards {
		fmt.Printf("  [%s] runs=%d findings=%d poc_hit=%v\n", g.Guard, g.Runs, g.Findings, g.KnownPOCHit)
	}
	fmt.Printf("verdict=%s\nreport=%s\n", report.Verdict, outPath)
}

func runGuardPool(ctx context.Context, wasmHex string, bytesMode bool, poc []byte, pocU64 uint64, runs, workers int) (guardRow, error) {
	var row guardRow
	if bytesMode {
		row.Mode = "bytes"
		seeds := []any{}
		if len(poc) > 0 {
			seeds = append(seeds, hex.EncodeToString(poc))
		}
		cfg := poolfuzz.PilotBytesCorpusConfig(wasmHex, fuzzengine.DefaultMaxInputBytesStd, seeds, true)
		return runPoolCfg(ctx, cfg, "bytes", runs, workers, poc, row)
	}
	row.Mode = "u64"
	cfg := poolfuzz.PilotScriptPushGuidedConfig(wasmHex)
	if pocU64 != 0 {
		cfg["seed_corpus"] = []any{uint64(0), uint64(1), pocU64}
	}
	return runPoolCfg(ctx, cfg, "u64", runs, workers, poc, row)
}

func runPoolCfg(ctx context.Context, cfg map[string]any, mode string, runs, workers int, poc []byte, row guardRow) (guardRow, error) {
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("matrix-%d.db", time.Now().UnixNano()))
	_ = os.Remove(dbPath)
	db, err := store.Open(dbPath)
	if err != nil {
		return row, err
	}
	defer db.Close()
	defer os.Remove(dbPath)

	cid := fmt.Sprintf("matrix-%s-%d", mode, time.Now().Unix())
	svc := &poolfuzz.Service{DB: db}
	if err := svc.RegisterCampaign(ctx, poolfuzz.Campaign{
		ID: cid, CampaignType: "property", Title: "matrix",
		Status: "running", BudgetRuns: runs, BudgetSeconds: 3600, Config: cfg,
	}); err != nil {
		return row, err
	}
	wids := make([]string, workers)
	for i := range wids {
		wids[i] = fmt.Sprintf("mx-w%d", i+1)
	}
	now := time.Now().Unix()
	done, err := svc.LocalDrainCampaign(ctx, cid, runs, wids, now, func(wid string, w poolfuzz.ClaimedWork) error {
		var cr int32
		var trap string
		var execErr error
		if len(w.InputBytes) > 0 {
			cr, _, trap, execErr = poolfuzz.ExecuteLocallyBytes(ctx, w.WasmCheckHex, w.InputBytes, 800)
		} else {
			cr, _, trap, execErr = poolfuzz.ExecuteLocally(ctx, w.WasmCheckHex, w.ActualInput, 800)
		}
		if execErr != nil {
			return execErr
		}
		return svc.Submit(ctx, poolfuzz.SubmitRequest{
			WorkerID: wid, WorkID: w.WorkID, CampaignID: cid, ItemID: w.ItemID,
			InputN: w.InputN, ActualInput: w.ActualInput, InputBytes: w.InputBytes,
			CheckResult: cr, DurationMS: 1, Trap: trap,
		})
	})
	if err != nil {
		return row, err
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, cid).Scan(&row.Findings)
	row.Runs = done
	row.KnownPOCHit = row.Findings > 0
	return row, nil
}

func runFluxTapFilterTest(root string) caseResult {
	traceRoot := os.Getenv("FLUXTAP_ROOT")
	if traceRoot == "" {
		traceRoot = "/tmp/FluxTap"
	}
	if _, err := os.Stat(traceRoot); err != nil {
		return caseResult{Name: "fluxtap_native_panic", Category: "external", Note: "FluxTap not cloned"}
	}
	cmd := exec.Command("go", "test", "-count=1", "-v", "-run", "TestFilterMalformedOperatorPanic", "./internal/filter/")
	cmd.Dir = traceRoot
	out, err := cmd.CombinedOutput()
	hit := strings.Contains(string(out), "panic reproduced")
	note := "native Go panic not reproduced"
	if hit {
		note = "native Go panic confirmed (FluxTap filter.go)"
	} else if err != nil {
		note = strings.TrimSpace(string(out))
		if len(note) > 120 {
			note = note[:120]
		}
	}
	return caseResult{Name: "fluxtap_native_panic", Category: "external", Input: `\xc7=`, Hit: hit, Note: note}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func repoRoot() string {
	wd, _ := os.Getwd()
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "internal/poolfuzz")); err == nil {
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	return wd
}
