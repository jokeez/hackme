// fuzz-sandbox runs a local pool pilot (claim/submit loop) without VPS or prod deploy.
//
//	go run ./cmd/fuzz-sandbox -runs 256 -workers 3
//	go run ./cmd/fuzz-sandbox -compare -runs 128 -workers 2
//	go run ./cmd/fuzz-sandbox -bytes -seed-profile=tracefuse -wasm …/tracefuse_…_bytes_guard.wasm
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
	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

type sandboxResult struct {
	Mode       string `json:"mode"`
	InputMode  string `json:"input_mode"`
	Runs       int    `json:"runs_done"`
	Findings   int    `json:"findings"`
	PoolCorpus int    `json:"pool_corpus"`
	CampaignID string `json:"campaign_id"`
	ElapsedMS  int64  `json:"elapsed_ms"`
}

func main() {
	runs := flag.Int("runs", 64, "work items to complete")
	workers := flag.Int("workers", 2, "parallel worker ids")
	linear := flag.Bool("linear", false, "linear control only")
	bytesMode := flag.Bool("bytes", false, "byte input_mode pilot (P4)")
	seedProfile := flag.String("seed-profile", "auto", "bytes seeds: auto|script_push|tracefuse|fluxtap")
	maxInput := flag.Int("max-input-bytes", fuzzengine.DefaultMaxInputBytesStd, "bytes mode max_input_bytes (256/1024/4096)")
	compare := flag.Bool("compare", false, "run linear then guided and print both")
	jsonOut := flag.String("json-out", "", "write result JSON to file")
	dbPath := flag.String("db", "", "sqlite path (default temp dir)")
	wasmPath := flag.String("wasm", "", "wasm file (default tasks/artifacts/security/rust_script_push_bounds_guard.wasm)")
	flag.Parse()

	root := repoRoot()
	wasmFile := *wasmPath
	if wasmFile == "" {
		wasmFile = filepath.Join(root, "tasks", "artifacts", "security", "rust_script_push_bounds_guard.wasm")
	}
	wasmHex, err := loadWasmHex(root, wasmFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm: %v\n", err)
		os.Exit(1)
	}
	profile := resolveSeedProfile(*seedProfile, wasmFile)

	if *compare {
		linearRes, err := runSandbox(wasmHex, *runs, *workers, true, *bytesMode, *dbPath, profile, *maxInput)
		if err != nil {
			fmt.Fprintf(os.Stderr, "linear: %v\n", err)
			os.Exit(1)
		}
		guidedRes, err := runSandbox(wasmHex, *runs, *workers, false, *bytesMode, *dbPath, profile, *maxInput)
		if err != nil {
			fmt.Fprintf(os.Stderr, "guided: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\n===== SANDBOX COMPARE runs=%d workers=%d bytes=%v seed=%s max_input=%d =====\n",
			*runs, *workers, *bytesMode, profile, *maxInput)
		printResult("linear", linearRes)
		printResult("guided", guidedRes)
		if *jsonOut != "" {
			payload, _ := json.MarshalIndent(map[string]any{"linear": linearRes, "guided": guidedRes, "seed_profile": profile}, "", "  ")
			_ = os.WriteFile(*jsonOut, payload, 0o644)
		}
		return
	}

	res, err := runSandbox(wasmHex, *runs, *workers, *linear, *bytesMode, *dbPath, profile, *maxInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sandbox: %v\n", err)
		os.Exit(1)
	}
	printResult(res.Mode, res)
	if *jsonOut != "" {
		payload, _ := json.MarshalIndent(res, "", "  ")
		_ = os.WriteFile(*jsonOut, payload, 0o644)
	}
}

func runSandbox(wasmHex string, runs, workers int, linear, bytesMode bool, dbPath, seedProfile string, maxInput int) (sandboxResult, error) {
	var out sandboxResult
	dir := dbPath
	if dir == "" {
		d, err := os.MkdirTemp("", "fuzz-sandbox-*")
		if err != nil {
			return out, err
		}
		dir = filepath.Join(d, "sandbox.db")
	}
	db, err := store.Open(dir)
	if err != nil {
		return out, err
	}
	defer db.Close()

	var cfg map[string]any
	if bytesMode {
		cfg = poolfuzz.PilotBytesCorpusConfig(wasmHex, maxInput, seedsForProfile(seedProfile), true)
		out.InputMode = "bytes"
		// Short CVE-class seeds (e.g. \xc7=) are destroyed by heavy bitflip rounds on linear.
		if seedProfile == "fluxtap" {
			cfg["mutation_rounds"] = 2
		}
	} else {
		cfg = poolfuzz.PilotScriptPushGuidedConfig(wasmHex)
		out.InputMode = "u64"
	}
	if linear {
		delete(cfg, "guided_scheduling")
		out.Mode = "linear"
	} else {
		out.Mode = "guided"
	}
	campaignID := fmt.Sprintf("local-sandbox-%s-%s", out.Mode, out.InputMode)

	svc := &poolfuzz.Service{DB: db}
	ctx := context.Background()
	start := time.Now()
	if err := svc.RegisterCampaign(ctx, poolfuzz.Campaign{
		ID: campaignID, CampaignType: "property", Title: "local sandbox",
		Status: "running", BudgetRuns: runs, BudgetSeconds: 7200, Config: cfg,
	}); err != nil {
		return out, err
	}
	if err := svc.EnsureWorkItems(ctx, campaignID, time.Now().Unix()); err != nil {
		return out, err
	}

	workerIDs := workerNames(workers)
	now := time.Now().Unix()
	done, err := svc.LocalDrainCampaign(ctx, campaignID, runs, workerIDs, now, func(wid string, w poolfuzz.ClaimedWork) error {
		var cr int32
		var trap string
		var err error
		if len(w.InputBytes) > 0 {
			cr, _, trap, err = poolfuzz.ExecuteLocallyBytes(ctx, w.WasmCheckHex, w.InputBytes, 800)
		} else {
			cr, _, trap, err = poolfuzz.ExecuteLocally(ctx, w.WasmCheckHex, w.ActualInput, 800)
		}
		if err != nil {
			return err
		}
		return svc.Submit(ctx, poolfuzz.SubmitRequest{
			WorkerID: wid, WorkID: w.WorkID, CampaignID: w.CampaignID,
			ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput,
			InputBytes: w.InputBytes, CheckResult: cr, DurationMS: 1, Trap: trap,
		})
	})
	if err != nil {
		return out, err
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&out.Findings)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_pool_corpus WHERE campaign_id=?`, campaignID).Scan(&out.PoolCorpus)
	out.Runs = done
	out.CampaignID = campaignID
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, nil
}

func printResult(label string, r sandboxResult) {
	fmt.Printf("  [%s] runs=%d findings=%d corpus=%d elapsed_ms=%d input_mode=%s\n",
		label, r.Runs, r.Findings, r.PoolCorpus, r.ElapsedMS, r.InputMode)
}

func workerNames(n int) []string {
	if n <= 0 {
		n = 1
	}
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("sandbox-worker-%d", i+1)
	}
	return out
}

func repoRoot() string {
	wd, _ := os.Getwd()
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "internal", "poolfuzz")); err == nil {
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	return wd
}

func loadWasmHex(root, path string) (string, error) {
	if path == "" {
		path = filepath.Join(root, "tasks", "artifacts", "security", "rust_script_push_bounds_guard.wasm")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func resolveSeedProfile(flagVal, wasmPath string) string {
	v := strings.ToLower(strings.TrimSpace(flagVal))
	if v != "" && v != "auto" {
		return v
	}
	base := strings.ToLower(filepath.Base(wasmPath))
	switch {
	case strings.Contains(base, "tracefuse"):
		return "tracefuse"
	case strings.Contains(base, "fluxtap"):
		return "fluxtap"
	default:
		return "script_push"
	}
}

func seedsForProfile(profile string) []any {
	switch strings.ToLower(strings.TrimSpace(profile)) {
	case "tracefuse":
		return poolfuzz.TracefuseByteSeeds()
	case "fluxtap":
		return poolfuzz.FluxtapFilterByteSeeds()
	default:
		return nil // PilotBytesCorpusConfig fills script_push defaults
	}
}
