// tracefuse_e2e — local HackMe engine vs Tracefuse repo (demo-vulnerable).
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

	"hackme/internal/poolfuzz"
	"hackme/internal/sandbox"
	"hackme/internal/store"
)

func main() {
	root := repoRoot()
	traceRoot := os.Getenv("TRACEFUSE_ROOT")
	if traceRoot == "" {
		traceRoot = "/tmp/Tracefuse"
	}
	demo := filepath.Join(traceRoot, "examples", "demo-vulnerable")

	u64Wasm, _ := os.ReadFile(filepath.Join(root, "tasks/artifacts/security/rust_tracefuse_detector_guard.wasm"))
	bytesWasm, _ := os.ReadFile(filepath.Join(root, "tasks/artifacts/security/rust_tracefuse_detector_bytes_guard.wasm"))
	ctx := context.Background()

	type lineHit struct {
		Source string `json:"source"`
		Line   string `json:"line"`
		Len    int    `json:"len"`
		U64Hit bool   `json:"u64_hit"`
		BytesHit bool `json:"bytes_hit"`
	}
	var direct []lineHit

	files := []string{
		filepath.Join(demo, ".env"),
		filepath.Join(demo, "Dockerfile"),
		filepath.Join(demo, ".github/workflows/ci.yml"),
	}
	for _, fp := range files {
		raw, err := os.ReadFile(fp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", fp, err)
			continue
		}
		rel, _ := filepath.Rel(traceRoot, fp)
		for _, line := range strings.Split(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if len(line) > 4096 {
				line = line[:4096]
			}
			var u64Hit, bytesHit bool
			if len(u64Wasm) > 0 {
				u64Hit, _ = sandbox.InvokeCheck(ctx, u64Wasm, packLE8(line))
			}
			if len(bytesWasm) > 0 {
				bytesHit, _ = sandbox.InvokeCheckInput(ctx, bytesWasm, []byte(line))
			}
			direct = append(direct, lineHit{Source: rel, Line: trunc(line, 80), Len: len(line), U64Hit: u64Hit, BytesHit: bytesHit})
		}
	}

	u64Hit, u64Miss, bytesHit, bytesMiss := 0, 0, 0, 0
	for _, d := range direct {
		if d.BytesHit {
			bytesHit++
		} else {
			bytesMiss++
		}
		if d.U64Hit {
			u64Hit++
		} else {
			u64Miss++
		}
	}

	wasmHex := hex.EncodeToString(bytesWasm)
	seeds := poolfuzz.TracefuseByteSeeds()
	// add Dockerfile lines from repo
	for _, d := range direct {
		if d.Source == "examples/demo-vulnerable/Dockerfile" && !d.BytesHit {
			seeds = append(seeds, d.Line)
		}
	}
	poolLin, _ := runPool(wasmHex, 4096, seeds, true, 256, 3)
	poolGui, _ := runPool(wasmHex, 4096, seeds, false, 256, 3)

	tracefuseSummary := runTracefuseScan(traceRoot, demo)

	report := map[string]any{
		"generated_at":      time.Now().UTC().Format(time.RFC3339),
		"upstream":          "https://github.com/FounderB/Tracefuse",
		"demo_path":         demo,
		"engine":            "HackMe poolfuzz local (P4 bytes + guided)",
		"direct_line_scan":  direct,
		"direct_summary": map[string]any{
			"lines": len(direct), "u64_hits": u64Hit, "u64_miss": u64Miss,
			"bytes_hits": bytesHit, "bytes_miss": bytesMiss,
		},
		"pool_bytes_4096_linear": poolLin,
		"pool_bytes_4096_guided": poolGui,
		"tracefuse_cli_scan": tracefuseSummary,
	}
	out := filepath.Join(root, "tasks/artifacts/security/tracefuse_e2e_report.json")
	b, _ := json.MarshalIndent(report, "", "  ")
	_ = os.WriteFile(out, b, 0o644)

	fmt.Println("===== Tracefuse × HackMe E2E =====")
	fmt.Printf("direct lines: %d | u64 hits: %d | bytes hits: %d\n", len(direct), u64Hit, bytesHit)
	fmt.Printf("pool 4096 linear: runs=%d findings=%d\n", poolLin.Runs, poolLin.Findings)
	fmt.Printf("pool 4096 guided: runs=%d findings=%d corpus=%d\n", poolGui.Runs, poolGui.Findings, poolGui.Corpus)
	if ts, ok := tracefuseSummary["total_findings"]; ok {
		fmt.Printf("tracefuse CLI (full repo scan): %v findings\n", ts)
	}
	fmt.Printf("report: %s\n", out)
}

func runTracefuseScan(traceRoot, demo string) map[string]any {
	bin := filepath.Join(traceRoot, "target/release/tracefuse")
	if _, err := os.Stat(bin); err != nil {
		return map[string]any{"skipped": "tracefuse binary not built"}
	}
	out, cmdErr := runCmd(filepath.Join(traceRoot), bin, "scan", demo, "--json")
	if strings.TrimSpace(out) == "" {
		if cmdErr != nil {
			return map[string]any{"error": cmdErr.Error()}
		}
		return map[string]any{"error": "empty tracefuse output"}
	}
	var parsed map[string]any
	if json.Unmarshal([]byte(out), &parsed) != nil {
		return map[string]any{"raw_len": len(out), "parse_error": true}
	}
	summary, _ := parsed["summary"].(map[string]any)
	total := 0
	if summary != nil {
		for _, k := range []string{"critical", "high", "medium", "low", "info"} {
			if v, ok := summary[k]; ok {
				switch x := v.(type) {
				case float64:
					total += int(x)
				}
			}
		}
	}
	result := map[string]any{"total_findings": total, "summary": summary, "score": parsed["score"]}
	if cmdErr != nil {
		// Tracefuse exits 1 when findings exist; JSON on stdout is still valid.
		result["exit_note"] = cmdErr.Error()
	}
	return result
}

func runCmd(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	b, err := cmd.CombinedOutput()
	return string(b), err
}

type poolRes struct {
	Runs, Findings, Corpus int
	ElapsedMS              int64
}

func runPool(wasmHex string, maxInput int, seeds []any, linear bool, runs, workers int) (poolRes, error) {
	var out poolRes
	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("tf-e2e-%d-%v.db", maxInput, linear))
	_ = os.Remove(dbPath)
	db, err := store.Open(dbPath)
	if err != nil {
		return out, err
	}
	defer db.Close()
	defer os.Remove(dbPath)

	cfg := poolfuzz.PilotBytesCorpusConfig(wasmHex, maxInput, seeds, !linear)
	mode := "guided"
	if linear {
		mode = "linear"
	}
	cid := fmt.Sprintf("tracefuse-e2e-%s-%d", mode, maxInput)
	svc := &poolfuzz.Service{DB: db}
	ctx := context.Background()
	start := time.Now()
	_ = svc.RegisterCampaign(ctx, poolfuzz.Campaign{
		ID: cid, CampaignType: "property", Title: "tracefuse e2e",
		Status: "running", BudgetRuns: runs, BudgetSeconds: 7200, Config: cfg,
	})
	_ = svc.EnsureWorkItems(ctx, cid, time.Now().Unix())
	wids := make([]string, workers)
	for i := range wids {
		wids[i] = fmt.Sprintf("tf-w%d", i+1)
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
		return out, err
	}
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, cid).Scan(&out.Findings)
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_pool_corpus WHERE campaign_id=?`, cid).Scan(&out.Corpus)
	out.Runs = done
	out.ElapsedMS = time.Since(start).Milliseconds()
	return out, nil
}

func packLE8(s string) uint64 {
	b := append([]byte(s), make([]byte, 8)...)[:8]
	var u uint64
	for i := 0; i < 8; i++ {
		u |= uint64(b[i]) << (8 * i)
	}
	return u
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
