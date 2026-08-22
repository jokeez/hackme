// tracefuse_sandbox_run — A/B fuzz Tracefuse WASM guard via real poolfuzz sandbox path.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func main() {
	root := repoRoot()
	wasmPath := filepath.Join(root, "tasks", "artifacts", "security", "rust_tracefuse_detector_guard.wasm")
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wasm: %v\n", err)
		os.Exit(1)
	}
	wasmHex := hex.EncodeToString(raw)

	runs := 128
	workers := 3
	dbPath := filepath.Join(os.TempDir(), "tracefuse-fuzz-report.db")
	_ = os.Remove(dbPath)

	linear, err := runArm(wasmHex, dbPath, runs, workers, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "linear: %v\n", err)
		os.Exit(1)
	}
	guided, err := runArm(wasmHex, dbPath, runs, workers, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "guided: %v\n", err)
		os.Exit(1)
	}

	report, err := buildReport(dbPath, linear, guided, wasmPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "report: %v\n", err)
		os.Exit(1)
	}
	outJSON := filepath.Join(root, "tasks", "artifacts", "security", "tracefuse_fuzz_report.json")
	outHTML := filepath.Join(root, "tasks", "artifacts", "security", "tracefuse_fuzz_report.html")
	jb, _ := json.MarshalIndent(report, "", "  ")
	_ = os.WriteFile(outJSON, jb, 0o644)
	_ = os.WriteFile(outHTML, []byte(renderHTML(report, linear, guided)), 0o644)

	fmt.Printf("\n===== TRACEFUSE × HackMe SANDBOX =====\n")
	fmt.Printf("target: %s\n", wasmPath)
	fmt.Printf("linear: runs=%d findings=%d corpus=%d elapsed_ms=%d\n", linear.Runs, linear.Findings, linear.Corpus, linear.ElapsedMS)
	fmt.Printf("guided: runs=%d findings=%d corpus=%d elapsed_ms=%d\n", guided.Runs, guided.Findings, guided.Corpus, guided.ElapsedMS)
	fmt.Printf("report: %s\n", outJSON)
	fmt.Printf("report: %s\n", outHTML)
}

type armResult struct {
	Mode       string `json:"mode"`
	CampaignID string `json:"campaign_id"`
	Runs       int    `json:"runs"`
	Findings   int    `json:"findings"`
	Corpus     int    `json:"corpus"`
	ElapsedMS  int64  `json:"elapsed_ms"`
}

func tracefusePilotConfig(wasmHex string, linear bool) map[string]any {
	cfg := fuzzengine.NormalizeCampaignConfig(map[string]any{
		"pool_distributed":     true,
		"check_semantics":      "detector",
		"wasm_check_hex":       wasmHex,
		"guided_scheduling":    !linear,
		"pool_corpus_max":      256,
		"power_mut_cap":        4,
		"mutation_rounds":      4,
		"stable_crash_buckets": true,
		// Known Tracefuse demo-vulnerable patterns (8-byte LE windows)
		"seed_corpus": []any{
			0,
			1,
			0x4149414b,             // AKIA (LE)
			0x5f706867,             // ghp_
			0x6574616c3a,           // :late
			0x76696c5f6b73,         // sk_liv
			0x74736e6974736f70,     // postinst
			0x7165725f6c6c7570,     // pull_req (LE)
		},
	}, "property")
	return cfg
}

func runArm(wasmHex, dbPath string, runs, workers int, linear bool) (armResult, error) {
	var out armResult
	if linear {
		out.Mode = "linear"
	} else {
		out.Mode = "guided"
	}
	out.CampaignID = fmt.Sprintf("tracefuse-sandbox-%s-u64", out.Mode)

	db, err := store.Open(dbPath)
	if err != nil {
		return out, err
	}
	defer db.Close()

	cfg := tracefusePilotConfig(wasmHex, linear)
	svc := &poolfuzz.Service{DB: db}
	ctx := context.Background()
	start := time.Now()
	if err := svc.RegisterCampaign(ctx, poolfuzz.Campaign{
		ID: out.CampaignID, CampaignType: "property",
		Title: "Tracefuse detector guard (FounderB/Tracefuse patterns)",
		Status: "running", BudgetRuns: runs, BudgetSeconds: 7200, Config: cfg,
	}); err != nil {
		return out, err
	}
	if err := svc.EnsureWorkItems(ctx, out.CampaignID, time.Now().Unix()); err != nil {
		return out, err
	}

	workerIDs := make([]string, workers)
	for i := 0; i < workers; i++ {
		workerIDs[i] = fmt.Sprintf("tracefuse-worker-%d", i+1)
	}
	done := 0
	for done < runs {
		progress := false
		for _, wid := range workerIDs {
			if done >= runs {
				break
			}
			w, ok, err := svc.Claim(ctx, wid, time.Now().Unix())
			if err != nil {
				return out, err
			}
			if !ok || w.CampaignID != out.CampaignID {
				continue
			}
			progress = true
			cr, _, trap, err := poolfuzz.ExecuteLocally(ctx, w.WasmCheckHex, w.ActualInput, 800)
			if err != nil {
				return out, err
			}
			if err := svc.Submit(ctx, poolfuzz.SubmitRequest{
				WorkerID: wid, WorkID: w.WorkID, CampaignID: w.CampaignID,
				ItemID: w.ItemID, InputN: w.InputN, ActualInput: w.ActualInput,
				InputBytes: w.InputBytes, CheckResult: cr, DurationMS: 1, Trap: trap,
			}); err != nil {
				return out, err
			}
			done++
		}
		if !progress {
			break
		}
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, out.CampaignID).Scan(&out.Findings)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_pool_corpus WHERE campaign_id=?`, out.CampaignID).Scan(&out.Corpus)
	out.Runs = done
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, nil
}

type findingRow struct {
	ID          string `json:"id"`
	CampaignID  string `json:"campaign_id"`
	FindingType string `json:"finding_type"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	ReproCmd    string `json:"repro_cmd"`
	InputHex    string `json:"input_hex"`
	CreatedAt   int64  `json:"created_at"`
}

func buildReport(dbPath string, linear, guided armResult, wasmPath string) (map[string]any, error) {
	db, err := store.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	ctx := context.Background()

	loadFindings := func(campaignID string) ([]findingRow, error) {
		rows, err := db.QueryContext(ctx, `
			SELECT id, campaign_id, finding_type, severity, title, repro_cmd, input_sha256, created_at
			FROM fuzz_findings WHERE campaign_id=? ORDER BY created_at ASC LIMIT 50`, campaignID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []findingRow
		for rows.Next() {
			var f findingRow
			var sha string
			if err := rows.Scan(&f.ID, &f.CampaignID, &f.FindingType, &f.Severity, &f.Title, &f.ReproCmd, &sha, &f.CreatedAt); err != nil {
				return nil, err
			}
			_ = sha
			out = append(out, f)
		}
		return out, rows.Err()
	}
	lf, _ := loadFindings(linear.CampaignID)
	gf, _ := loadFindings(guided.CampaignID)

	return map[string]any{
		"report_version": "tracefuse_hackme_sandbox_v1",
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"upstream":       "https://github.com/FounderB/Tracefuse",
		"wasm_guard":     wasmPath,
		"fuzzer_path":    "poolfuzz claim/submit + sandbox.InvokeCheck + guided_scheduling",
		"compare": map[string]any{
			"linear":  linear,
			"guided":  guided,
			"runs_requested": 128,
			"workers": 3,
		},
		"findings_linear":  lf,
		"findings_guided":  gf,
		"notes": []string{
			"Guard ports Tracefuse secret/dockerfile/npm/ci heuristics into check(i64) 8-byte window.",
			"Seeded with patterns from Tracefuse examples/demo-vulnerable.",
		},
	}, nil
}

func renderHTML(report map[string]any, lin, guid armResult) string {
	return fmt.Sprintf(`<!DOCTYPE html><html><head><meta charset="utf-8"><title>Tracefuse × HackMe Fuzz Report</title>
<style>body{font-family:system-ui;background:#0f172a;color:#e2e8f0;padding:2rem}table{border-collapse:collapse;width:100%%;margin:1rem 0}td,th{border:1px solid #334155;padding:.5rem .75rem}th{background:#1e293b}code{color:#7dd3fc}</style></head><body>
<h1>Tracefuse × HackMe pool fuzz</h1>
<p>Upstream: <a href="https://github.com/FounderB/Tracefuse">FounderB/Tracefuse</a> · Fuzzer: real <code>poolfuzz</code> sandbox (claim/submit, escrow semantics, guided scheduling)</p>
<table><tr><th>Mode</th><th>Runs</th><th>Findings</th><th>Corpus</th><th>ms</th></tr>
<tr><td>linear</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td></tr>
<tr><td>guided</td><td>%d</td><td>%d</td><td>%d</td><td>%d</td></tr></table>
<p>WASM: <code>%s</code></p>
<p>Generated: %s</p>
</body></html>`,
		lin.Runs, lin.Findings, lin.Corpus, lin.ElapsedMS,
		guid.Runs, guid.Findings, guid.Corpus, guid.ElapsedMS,
		html.EscapeString(report["wasm_guard"].(string)),
		html.EscapeString(report["generated_at"].(string)),
	)
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
