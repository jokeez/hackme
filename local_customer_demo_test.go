package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
)

// TestLocalCustomerDemo2048 — full local product demo (report HTML, gate, proof).
// Run: HACKME_LOCAL_DEMO=1 go test -count=1 -timeout=15m -run TestLocalCustomerDemo2048 .
func TestLocalCustomerDemo2048(t *testing.T) {
	if os.Getenv("HACKME_LOCAL_DEMO") != "1" {
		t.Skip("set HACKME_LOCAL_DEMO=1 to generate demo artifacts")
	}
	const targetExecs = 2048
	packID := strings.TrimSpace(os.Getenv("HACKME_DEMO_PACK"))
	if packID == "" {
		packID = "secrets"
	}
	outDir := strings.TrimSpace(os.Getenv("HACKME_DEMO_OUT"))
	if outDir == "" {
		outDir = filepath.Join("reports", "local-demo-2048")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	root := repoRootForPackE2E(t)
	pack, err := fuzzingcli.GuardPackFor(packID)
	if err != nil {
		t.Fatal(err)
	}
	wasmPath := filepath.Join(root, pack.WasmRelPath)
	if err := buildPackWasmIfMissing(root, pack, wasmPath); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}

	a, db := newWalletTestApp(t)
	addr, _, _ := a.chain.Wallet(context.Background())
	units := chain.HMCToUnits(50)
	_, _ = db.ExecContext(context.Background(), `UPDATE wallet SET balance_hmc=50, balance_units=? WHERE id=1`, units)
	_, _ = db.ExecContext(context.Background(), `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr)
	t.Setenv("HACKME_ADMIN_TOKEN", "local-demo-admin")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	t.Setenv("HACKME_FUZZ_AUTORUN", "0")

	pkg, err := fuzzingcli.B2BPackageFor("deep")
	if err != nil {
		t.Fatal(err)
	}
	pkg = fuzzingcli.AdjustPackageForPack(pkg, pack)
	cfg := fuzzingcli.ApplyPackConfig(map[string]any{}, pack)
	cfg["depth_tier"] = string(pkg.DepthTier)
	cfg = fuzzengine.ApplyDepthTier(cfg, pkg.DepthTier)
	cfg = fuzzengine.NormalizeCampaignConfig(cfg, "property")
	execPer := fuzzengine.ExecPerUnit(cfg)
	if v := strings.TrimSpace(os.Getenv("HACKME_DEMO_EXEC_PER_UNIT")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			execPer = n
			cfg["exec_per_unit"] = n
		}
	}
	budgetRuns := targetExecs
	if execPer > 1 {
		budgetRuns = (targetExecs + execPer - 1) / execPer
		if budgetRuns < 8 {
			budgetRuns = 8
			execPer = (targetExecs + budgetRuns - 1) / budgetRuns
			cfg["exec_per_unit"] = execPer
		}
	}

	const campaignID = "campaign-local-demo-2048"
	body := map[string]any{
		"title":            fmt.Sprintf("Local demo · %s · %d work × %d exec", pack.Title, budgetRuns, execPer),
		"campaign_id":      campaignID,
		"order_id":         "order-local-demo-2048",
		"budget_hmc":       pkg.BudgetHMC,
		"budget_runs":      budgetRuns,
		"budget_seconds":   pkg.BudgetSeconds,
		"wasm_check_hex":   hex.EncodeToString(raw),
		"create_poh_order": false,
		"pool_distributed": false,
		"public_proof":     true,
		"depth_tier":       string(pkg.DepthTier),
		"guard_pack":       pack.ID,
		"guard_name":       pack.ID,
		"input_mode":       cfg["input_mode"],
		"max_input_bytes":  cfg["max_input_bytes"],
		"guided_scheduling": cfg["guided_scheduling"],
		"mutation_rounds":  cfg["mutation_rounds"],
		"seed_byte_corpus": cfg["seed_byte_corpus"],
		"worker_batch":     128,
		"check_semantics":  "detector",
		"exec_per_unit":    execPer,
	}
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/security-audit", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "local-demo-admin")
	rec := httptest.NewRecorder()
	a.rlHits = make(map[string]rateSlot)
	a.rlBan = make(map[string]int64)
	a.handleSecurityAudit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("security-audit %d: %s", rec.Code, rec.Body.String())
	}
	var wizardResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &wizardResp); err != nil {
		t.Fatal(err)
	}
	reportTok, _ := wizardResp["customer_report_token"].(string)
	if reportTok == "" {
		t.Fatalf("missing token: %v", wizardResp)
	}
	writeJSONFile(t, filepath.Join(outDir, "wizard-response.json"), wizardResp)

	ctx := context.Background()
	start := time.Now()
	for {
		if err := a.fuzzAutoRunnerTick(ctx); err != nil {
			t.Logf("tick: %v", err)
		}
		var done int
		_ = db.QueryRowContext(ctx,
			`SELECT COALESCE(json_extract(summary_json,'$.runs_done'),0) FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&done)
		if done >= budgetRuns {
			break
		}
		if time.Since(start) > 12*time.Minute {
			t.Fatalf("timeout runs_done=%d want %d", done, budgetRuns)
		}
	}
	elapsed := time.Since(start)
	t.Logf("completed %d work items (exec_per_unit=%d, ~%d wasm execs) in %s", budgetRuns, execPer, budgetRuns*execPer, elapsed.Round(time.Millisecond))

	report, err := a.buildFuzzReport(ctx, campaignID, 200)
	if err != nil {
		t.Fatal(err)
	}
	htmlOut := renderFuzzReportHTML(report)
	if err := os.WriteFile(filepath.Join(outDir, "report.html"), []byte(htmlOut), 0o644); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(outDir, "report.json"), report)

	gateReq := httptest.NewRequest(http.MethodGet,
		"/api/fuzz/campaigns/"+campaignID+"/gate?max_critical=0&max_high=0", nil)
	gateReq.Header.Set("X-Hackme-Report-Token", reportTok)
	gateRec := httptest.NewRecorder()
	a.handleFuzzCampaignGate(gateRec, gateReq, campaignID)
	if gateRec.Code != http.StatusOK {
		t.Fatalf("gate %d: %s", gateRec.Code, gateRec.Body.String())
	}
	var gate map[string]any
	if err := json.Unmarshal(gateRec.Body.Bytes(), &gate); err != nil {
		t.Fatal(err)
	}
	writeJSONFile(t, filepath.Join(outDir, "gate.json"), gate)

	c, _ := a.getFuzzCampaign(ctx, campaignID)
	proof := buildProofOfFuzz(c, report)
	writeJSONFile(t, filepath.Join(outDir, "proof.json"), proof)
	if err := os.WriteFile(filepath.Join(outDir, "proof.html"), []byte(renderProofOfFuzzHTML(proof)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "badge.svg"), []byte(renderProofOfFuzzBadgeSVG(proof)), 0o644); err != nil {
		t.Fatal(err)
	}

	index := demoIndexHTML(outDir, campaignID, pack.ID, budgetRuns, execPer, gate, elapsed)
	if err := os.WriteFile(filepath.Join(outDir, "index.html"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}

	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&findings)
	t.Logf("demo artifacts → %s (findings=%d gate_pass=%v)", outDir, findings, gate["pass"])
}

func buildPackWasmIfMissing(root string, pack fuzzingcli.GuardPack, wasmPath string) error {
	if _, err := os.Stat(wasmPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(wasmPath), 0o755); err != nil {
		return err
	}
	src := filepath.Join(root, pack.SourceRelPath)
	cmd := exec.Command("rustc", "--target", "wasm32-unknown-unknown", "-O", "--crate-type=cdylib", src, "-o", wasmPath)
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build wasm: %w\n%s", err, out)
	}
	return nil
}

func writeJSONFile(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func demoIndexHTML(outDir, campaignID, pack string, budgetRuns, execPer int, gate map[string]any, elapsed time.Duration) string {
	pass := gate["pass"] == true
	gateLabel := "FAIL"
	gateColor := "#ff6060"
	if pass {
		gateLabel = "PASS"
		gateColor = "#39ff14"
	}
	abs, _ := filepath.Abs(outDir)
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en"><head><meta charset="utf-8"/>
<title>HackMe Local Demo · %d work × %d exec</title>
<style>
body{font-family:ui-monospace,Menlo,monospace;background:#070b12;color:#c8d6e5;margin:0;padding:2rem;line-height:1.5}
h1{color:#00d1ff;font-size:1.2rem}
a{color:#39ff14}.card{border:1px solid rgba(0,209,255,.25);border-radius:12px;padding:1rem;margin:1rem 0;max-width:720px}
.gate{display:inline-block;padding:.4rem 1rem;border-radius:999px;font-weight:700;border:2px solid %s;color:%s}
.muted{color:#6b7c93;font-size:.85rem}
</style></head><body>
<h1>HackMe · local customer demo</h1>
<p class="muted">%s · pack <code>%s</code> · %s</p>
<div class="card">
<p><span class="gate">%s</span> crash-first gate · pass ≠ proven secure</p>
<ul>
<li><a href="report.html">report.html</a> — full deliverable (demo copy)</li>
<li><a href="gate.json">gate.json</a> — CI gate</li>
<li><a href="proof.html">proof.html</a> — Proof of Fuzz (public opt-in)</li>
<li><a href="badge.svg">badge.svg</a> — README badge</li>
<li><a href="wizard-response.json">wizard-response.json</a> — report token + URLs</li>
</ul>
<p class="muted">Runs: %d work items × %d exec/unit ≈ %d wasm execs · %s</p>
</div>
<p class="muted">Local only — not on hackme.tech</p>
</body></html>`, budgetRuns, execPer, gateColor, gateColor, campaignID, pack, elapsed.Round(time.Millisecond), gateLabel, budgetRuns, execPer, budgetRuns*execPer, abs)
}
