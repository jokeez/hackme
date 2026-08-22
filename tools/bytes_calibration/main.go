// bytes_calibration runs max_input_bytes matrix (256/1K/4K) on Tracefuse bytes guard via poolfuzz sandbox.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

type tierResult struct {
	MaxInputBytes int    `json:"max_input_bytes"`
	Mode          string `json:"mode"`
	Runs          int    `json:"runs"`
	Findings      int    `json:"findings"`
	Corpus        int    `json:"corpus"`
	ElapsedMS     int64  `json:"elapsed_ms"`
	P95MS         int64  `json:"p95_check_ms"`
}

func main() {
	root := repoRoot()
	wasmPath := filepath.Join(root, "tasks", "artifacts", "security", "rust_tracefuse_detector_bytes_guard.wasm")
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm: %v\n", err)
		os.Exit(1)
	}
	wasmHex := hex.EncodeToString(raw)
	ctx := context.Background()
	if err := sandbox.ValidateCheckWasm(ctx, raw); err != nil {
		fmt.Fprintf(os.Stderr, "validate: %v\n", err)
		os.Exit(1)
	}

	tiers := []int{256, 1024, 4096}
	modes := []struct {
		name   string
		linear bool
	}{{"linear", true}, {"guided", false}}

	var report []tierResult
	for _, maxB := range tiers {
		for _, m := range modes {
			res, err := runTier(wasmHex, maxB, m.linear, 128, 3)
			if err != nil {
				fmt.Fprintf(os.Stderr, "tier %d %s: %v\n", maxB, m.name, err)
				os.Exit(1)
			}
			res.MaxInputBytes = maxB
			res.Mode = m.name
			report = append(report, res)
			fmt.Printf("[%4d B %6s] runs=%d findings=%d corpus=%d p95=%dms elapsed=%dms\n",
				maxB, m.name, res.Runs, res.Findings, res.Corpus, res.P95MS, res.ElapsedMS)
		}
	}

	outDir := filepath.Join(root, "tasks", "artifacts", "security")
	_ = os.MkdirAll(outDir, 0o755)
	outPath := filepath.Join(outDir, "bytes_calibration_report.json")
	payload, _ := json.MarshalIndent(map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"sandbox_max_input_bytes": sandbox.MaxCheckInputBytes(),
		"wasm": wasmPath,
		"results": report,
	}, "", "  ")
	_ = os.WriteFile(outPath, payload, 0o644)
	fmt.Printf("\nreport: %s\n", outPath)
}

func runTier(wasmHex string, maxInput int, linear bool, runs, workers int) (tierResult, error) {
	var out tierResult
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("bytes-cal-%d-%v.db", maxInput, linear))
	_ = os.Remove(dbPath)
	db, err := store.Open(dbPath)
	if err != nil {
		return out, err
	}
	defer db.Close()
	defer os.Remove(dbPath)

	seeds := poolfuzz.TracefuseByteSeeds()
	cfg := poolfuzz.PilotBytesCorpusConfig(wasmHex, maxInput, seeds, !linear)
	campaignID := fmt.Sprintf("bytes-cal-%d-%s", maxInput, map[bool]string{true: "lin", false: "gui"}[linear])

	svc := &poolfuzz.Service{DB: db}
	ctx := context.Background()
	start := time.Now()
	if err := svc.RegisterCampaign(ctx, poolfuzz.Campaign{
		ID: campaignID, CampaignType: "property", Title: "bytes calibration",
		Status: "running", BudgetRuns: runs, BudgetSeconds: 7200, Config: cfg,
	}); err != nil {
		return out, err
	}
	if err := svc.EnsureWorkItems(ctx, campaignID, time.Now().Unix()); err != nil {
		return out, err
	}

	var durations []int64
	wids := make([]string, workers)
	for i := 0; i < workers; i++ {
		wids[i] = fmt.Sprintf("cal-worker-%d", i+1)
	}
	now := time.Now().Unix()
	done, err := svc.LocalDrainCampaign(ctx, campaignID, runs, wids, now, func(wid string, w poolfuzz.ClaimedWork) error {
		t0 := time.Now()
		var cr int32
		var trap string
		var execErr error
		if len(w.InputBytes) > 0 {
			cr, _, trap, execErr = poolfuzz.ExecuteLocallyBytes(ctx, w.WasmCheckHex, w.InputBytes, 800)
		} else {
			cr, _, trap, execErr = poolfuzz.ExecuteLocally(ctx, w.WasmCheckHex, w.ActualInput, 800)
		}
		durations = append(durations, time.Since(t0).Milliseconds())
		if execErr != nil {
			return execErr
		}
		return svc.Submit(ctx, poolfuzz.SubmitRequest{
			WorkerID: wid, WorkID: w.WorkID, CampaignID: w.CampaignID,
			ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput,
			InputBytes: w.InputBytes, CheckResult: cr, DurationMS: int(durations[len(durations)-1]), Trap: trap,
		})
	})
	if err != nil {
		return out, err
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&out.Findings)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_pool_corpus WHERE campaign_id=?`, campaignID).Scan(&out.Corpus)
	out.Runs = done
	out.ElapsedMS = time.Since(start).Milliseconds()
	out.P95MS = p95(durations)
	return out, nil
}

func p95(v []int64) int64 {
	if len(v) == 0 {
		return 0
	}
	cp := append([]int64(nil), v...)
	sortInt64(cp)
	idx := int(float64(len(cp)-1) * 0.95)
	return cp[idx]
}

func sortInt64(a []int64) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
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

var _ = fuzzengine.DefaultMaxInputBytes
