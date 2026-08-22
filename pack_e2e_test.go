package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/chain"
	"hackme/internal/fuzzingcli"
)

// TestPackSecretsE2EAuditReportExplain: wizard-equivalent security-audit → autorun → report/gate explain.
func TestPackSecretsE2EAuditReportExplain(t *testing.T) {
	root := repoRootForPackE2E(t)
	pack, err := fuzzingcli.GuardPackFor("secrets")
	if err != nil {
		t.Fatal(err)
	}
	wasmPath := filepath.Join(root, pack.WasmRelPath)
	if _, err := os.Stat(wasmPath); err != nil {
		src := filepath.Join(root, pack.SourceRelPath)
		_ = os.MkdirAll(filepath.Dir(wasmPath), 0o755)
		cmd := exec.Command("rustc", "--target", "wasm32-unknown-unknown", "-O", "--crate-type=cdylib", src, "-o", wasmPath)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("build wasm: %v\n%s", err, out)
		}
	}
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		t.Fatal(err)
	}

	a, db := newWalletTestApp(t)
	addr, _, _ := a.chain.Wallet(context.Background())
	units := chain.HMCToUnits(50)
	if _, err := db.ExecContext(context.Background(), `UPDATE wallet SET balance_hmc=50, balance_units=? WHERE id=1`, units); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE accounts SET balance_units=? WHERE address=?`, units, addr); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_ADMIN_TOKEN", "pack-e2e-admin")
	t.Setenv("HACKME_POOL_COORDINATOR_URL", "")
	t.Setenv("HACKME_FUZZ_AUTORUN", "0")

	pkg, err := fuzzingcli.B2BPackageFor("scan")
	if err != nil {
		t.Fatal(err)
	}
	pkg = fuzzingcli.AdjustPackageForPack(pkg, pack)
	cfg := fuzzingcli.ApplyPackConfig(map[string]any{}, pack)

	body := map[string]any{
		"title":             "pack-e2e-secrets",
		"campaign_id":       "campaign-pack-e2e-secrets",
		"order_id":          "order-pack-e2e-secrets",
		"budget_hmc":        1.0,
		"budget_runs":       24,
		"budget_seconds":    600,
		"wasm_check_hex":    hex.EncodeToString(raw),
		"create_poh_order":  false,
		"pool_distributed":  false,
		"depth_tier":        "bytes_corpus",
		"guard_pack":        pack.ID,
		"guard_name":        pack.ID,
		"input_mode":        cfg["input_mode"],
		"max_input_bytes":   cfg["max_input_bytes"],
		"guided_scheduling": cfg["guided_scheduling"],
		"mutation_rounds":   cfg["mutation_rounds"],
		"seed_byte_corpus":  cfg["seed_byte_corpus"],
	}
	_ = pkg
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/security-audit", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hackme-Admin-Token", "pack-e2e-admin")
	rec := httptest.NewRecorder()
	a.rlHits = make(map[string]rateSlot)
	a.rlBan = make(map[string]int64)
	a.handleSecurityAudit(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("security-audit %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	campaignID, _ := resp["campaign_id"].(string)
	reportTok, _ := resp["customer_report_token"].(string)
	if campaignID == "" || reportTok == "" {
		t.Fatalf("missing ids: %v", resp)
	}

	ctx := context.Background()
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if err := a.fuzzAutoRunnerTick(ctx); err != nil {
			t.Logf("autorun tick: %v", err)
		}
		var runs int
		_ = db.QueryRowContext(ctx, `SELECT COALESCE(json_extract(summary_json,'$.runs_done'),0) FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&runs)
		var findings int
		_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&findings)
		if findings >= 1 && runs >= 8 {
			break
		}
		if runs >= 24 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	var findings int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM fuzz_findings WHERE campaign_id=?`, campaignID).Scan(&findings)
	if findings < 1 {
		t.Fatalf("expected pack findings, got %d", findings)
	}

	report, err := a.buildFuzzReport(ctx, campaignID, 100)
	if err != nil {
		t.Fatal(err)
	}
	noise, _ := report["coverage_noise"].([]fuzzProductTopIssue)
	if len(noise) == 0 {
		t.Fatal("expected coverage_noise for detector pack")
	}
	explained := 0
	for _, n := range noise {
		if n.GuardPack != "secrets" {
			t.Fatalf("guard_pack=%q want secrets", n.GuardPack)
		}
		if strings.TrimSpace(n.Explain) != "" {
			explained++
		}
	}
	if explained == 0 {
		t.Fatalf("no explain on noise rows: %+v", noise[0])
	}

	gateReq := httptest.NewRequest(http.MethodGet, "/api/fuzz/campaigns/"+campaignID+"/gate?max_critical=0&max_high=0", nil)
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
	if gate["pass"] != true {
		t.Fatalf("detector noise must not fail crash gate: %v", gate)
	}
	samples, _ := gate["pack_explain_samples"].([]any)
	if len(samples) == 0 {
		t.Fatalf("gate missing pack_explain_samples: %v", gate)
	}
	gp, _ := gate["guard_pack"].(map[string]any)
	if gp == nil || gp["id"] != "secrets" {
		t.Fatalf("gate guard_pack=%v", gate["guard_pack"])
	}

	htmlOut := renderFuzzReportHTML(report)
	if !strings.Contains(htmlOut, "<th>Explain</th>") {
		t.Fatal("HTML missing Explain column")
	}
	t.Logf("E2E OK campaign=%s findings=%d explained=%d gate_samples=%d", campaignID, findings, explained, len(samples))
}

func repoRootForPackE2E(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "internal", "fuzzingcli")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repo root not found")
	return ""
}
