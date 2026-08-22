// pack_demo runs a local pool sandbox for a GuardPack and prints findings + explain.
// No VPS, no site deploy, no escrow — product demo only.
package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/poolfuzz"
	"hackme/internal/store"
)

func main() {
	packID := flag.String("pack", "secrets", "secrets|script_bounds|filter_utf8")
	runs := flag.Int("runs", 64, "pool runs")
	workers := flag.Int("workers", 2, "workers")
	jsonOut := flag.String("json-out", "", "write report JSON")
	flag.Parse()

	root := repoRoot()
	pack, err := fuzzingcli.GuardPackFor(*packID)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	wasmPath := filepath.Join(root, pack.WasmRelPath)
	if _, err := os.Stat(wasmPath); err != nil {
		fmt.Fprintf(os.Stderr, "[pack-demo] building %s\n", pack.ID)
		cmd := exec.Command("rustc", "--target", "wasm32-unknown-unknown", "-O", "--crate-type=cdylib",
			filepath.Join(root, pack.SourceRelPath), "-o", wasmPath)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "build: %v\n%s\n", err, out)
			os.Exit(1)
		}
	}
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	wasmHex := hex.EncodeToString(raw)

	cfg := fuzzingcli.ApplyPackConfig(map[string]any{
		"pool_distributed": true,
		"wasm_check_hex":   wasmHex,
		"check_semantics":  "detector",
	}, pack)
	cfg = fuzzengine.NormalizeCampaignConfig(cfg, "property")

	dbPath := filepath.Join(os.TempDir(), fmt.Sprintf("pack-demo-%s-%d.db", pack.ID, time.Now().UnixNano()))
	_ = os.Remove(dbPath)
	db, err := store.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer db.Close()
	defer os.Remove(dbPath)

	cid := fmt.Sprintf("pack-demo-%s", pack.ID)
	svc := &poolfuzz.Service{DB: db}
	ctx := context.Background()
	start := time.Now()
	if err := svc.RegisterCampaign(ctx, poolfuzz.Campaign{
		ID: cid, CampaignType: "property", Title: "pack demo · " + pack.Title,
		Status: "running", BudgetRuns: *runs, BudgetSeconds: 3600, Config: cfg,
	}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	wids := []string{"pack-w1", "pack-w2"}
	if *workers == 1 {
		wids = wids[:1]
	}
	now := time.Now().Unix()
	done, err := svc.LocalDrainCampaign(ctx, cid, *runs, wids, now, func(wid string, w poolfuzz.ClaimedWork) error {
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	type row struct {
		Title   string `json:"title"`
		Preview string `json:"input_preview"`
		Explain string `json:"explain"`
	}
	var findings []row
	q, err := db.QueryContext(ctx,
		`SELECT title, detail_json FROM fuzz_findings WHERE campaign_id=? ORDER BY created_at ASC LIMIT 12`, cid)
	if err == nil {
		defer q.Close()
		for q.Next() {
			var title, detail string
			if q.Scan(&title, &detail) != nil {
				continue
			}
			preview := previewFromDetail(detail)
			findings = append(findings, row{
				Title:   title,
				Preview: trunc(preview, 72),
				Explain: fuzzingcli.ExplainPackFinding(pack.ID, preview, title),
			})
		}
	}
	var total int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, cid).Scan(&total)

	report := map[string]any{
		"pack":          pack.ID,
		"pack_title":    pack.Title,
		"runs":          done,
		"findings":      total,
		"elapsed_ms":    time.Since(start).Milliseconds(),
		"input_mode":    pack.InputMode,
		"sample_issues": findings,
		"customer_flow": []string{
			"1. hackme-fuzzing packs",
			"2. hackme-fuzzing wizard --pack " + pack.ID + " --package audit",
			"3. Wait for miners / local sandbox",
			"4. Open report — each hit has explain guidance",
		},
	}
	b, _ := json.MarshalIndent(report, "", "  ")
	if *jsonOut != "" {
		_ = os.WriteFile(*jsonOut, b, 0o644)
	}

	fmt.Println("===== Pack demo:", pack.ID, "=====")
	fmt.Println(pack.Title)
	fmt.Printf("runs=%d findings=%d mode=%s\n\n", done, total, pack.InputMode)
	for i, f := range findings {
		fmt.Printf("%d) %s\n", i+1, trunc(f.Title, 80))
		fmt.Printf("   input:   %s\n", f.Preview)
		fmt.Printf("   explain: %s\n\n", f.Explain)
	}
	if total == 0 {
		fmt.Println("(no findings in this budget — try more runs or another pack)")
	}
	fmt.Println("Customer command:")
	fmt.Printf("  hackme-fuzzing wizard --pack %s --package audit\n", pack.ID)
}

func previewFromDetail(detailJSON string) string {
	var m map[string]any
	if json.Unmarshal([]byte(detailJSON), &m) != nil {
		return ""
	}
	if s, ok := m["input_hex"].(string); ok && s != "" {
		raw, err := hex.DecodeString(strings.TrimPrefix(strings.ToLower(s), "0x"))
		if err == nil && len(raw) > 0 {
			if mostlyPrint(raw) {
				return string(raw)
			}
			return "0x" + s
		}
		return s
	}
	if v, ok := m["actual_input"]; ok {
		switch n := v.(type) {
		case float64:
			return fmt.Sprintf("%d", uint64(n))
		case json.Number:
			return n.String()
		default:
			return fmt.Sprint(v)
		}
	}
	return ""
}

func mostlyPrint(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	n := 0
	for _, c := range b {
		if c == '\n' || c == '\t' || (c >= 32 && c < 127) {
			n++
		}
	}
	return n*2 >= len(b)
}

func trunc(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func repoRoot() string {
	wd, _ := os.Getwd()
	dir := wd
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "internal", "fuzzingcli")); err == nil {
				return dir
			}
		}
		dir = filepath.Dir(dir)
	}
	return wd
}
