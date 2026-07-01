package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"hackme/internal/fuzzingcli"
)

func doWizard(base string, args []string) error {
	fs := flag.NewFlagSet("wizard", flag.ExitOnError)
	pkgName := fs.String("package", "audit", "B2B package: scan|audit|deep")
	title := fs.String("title", "HackMe fuzz audit", "campaign title")
	wasmPath := fs.String("wasm", "", "path to guard .wasm (required)")
	payerRef := fs.String("payer-ref", "", "optional payer_ref for bookkeeping")
	publicOK := fs.Bool("allow-public-base", false, "allow non-loopback base (not recommended)")
	_ = fs.Parse(args)

	if !*publicOK && !fuzzingcli.IsLoopbackBase(base) {
		return fmt.Errorf("wizard refuses non-loopback base %q (use local node or --allow-public-base)", base)
	}
	if strings.TrimSpace(*wasmPath) == "" {
		return fmt.Errorf("usage: hackme-fuzzing wizard --wasm guard.wasm [--package scan|audit|deep]")
	}
	raw, err := os.ReadFile(*wasmPath)
	if err != nil {
		return err
	}
	pkg, err := fuzzingcli.B2BPackageFor(*pkgName)
	if err != nil {
		return err
	}
	wasmHex := hex.EncodeToString(raw)
	pool := pkg.PoolDistributed
	createPoH := pkg.CreatePoHOrder
	payload := map[string]any{
		"title":            strings.TrimSpace(*title),
		"depth_tier":       string(pkg.DepthTier),
		"wasm_check_hex":   wasmHex,
		"budget_hmc":       pkg.BudgetHMC,
		"budget_runs":      pkg.BudgetRuns,
		"pool_distributed": pool,
		"create_poh_order": createPoH,
	}
	if pkg.RewardHMC > 0 {
		payload["reward_hmc"] = pkg.RewardHMC
	}
	if pr := strings.TrimSpace(*payerRef); pr != "" {
		payload["payer_ref"] = pr
	}
	b, code, err := postSecurityAuditAuth(base, payload)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("POST /api/security-audit HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	var resp map[string]any
	if err := json.Unmarshal(b, &resp); err != nil {
		return err
	}
	campaignID, _ := resp["campaign_id"].(string)
	orderID, _ := resp["order_id"].(string)
	reportTok, _ := resp["customer_report_token"].(string)
	if campaignID == "" || reportTok == "" {
		fmt.Println(string(prettyJSON(b)))
		return fmt.Errorf("missing campaign_id or customer_report_token in response")
	}
	publicBase := strings.TrimSpace(os.Getenv("HACKME_PUBLIC_REPORT_BASE"))
	if publicBase == "" {
		publicBase = "https://hackme.tech"
	}
	publicBase = strings.TrimRight(publicBase, "/")
	reportURL := publicBase + "/api/fuzz/campaigns/" + campaignID + "/report.html"
	gateURL := publicBase + "/api/fuzz/campaigns/" + campaignID + "/gate?max_critical=0&max_high=0"
	pulseURL := publicBase + "/api/fuzz/campaigns/" + campaignID + "/pulse"

	out := map[string]any{
		"ok":                    true,
		"package":               pkg.Name,
		"depth_tier":            string(pkg.DepthTier),
		"budget_hmc":            pkg.BudgetHMC,
		"budget_runs":           pkg.BudgetRuns,
		"pool_distributed":      pool,
		"order_id":              orderID,
		"campaign_id":           campaignID,
		"customer_report_token": reportTok,
		"report_url":            reportURL,
		"gate_url":              gateURL,
		"pulse_url":             pulseURL,
		"ci_header":             "X-Hackme-Report-Token",
	}
	if v, ok := resp["pool_sync"]; ok {
		out["pool_sync"] = v
	}
	if v, ok := resp["pool_sync_warning"]; ok {
		out["pool_sync_warning"] = v
	}
	if v, ok := resp["order"]; ok {
		out["order"] = v
	}
	if v, ok := resp["escrow"]; ok {
		out["escrow"] = v
	}
	printJSON(out)
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintf(os.Stderr, "  1. Save report token securely (shown once).\n")
	fmt.Fprintf(os.Stderr, "  2. Watch progress: curl -H \"X-Hackme-Report-Token: …\" %s\n", pulseURL)
	fmt.Fprintf(os.Stderr, "  3. CI gate:       curl -H \"X-Hackme-Report-Token: …\" %s\n", gateURL)
	return nil
}

// doWizardDryRun builds the security-audit payload without calling the API (for tests).
func doWizardDryRun(pkgName, wasmPath string) (map[string]any, error) {
	pkg, err := fuzzingcli.B2BPackageFor(pkgName)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"depth_tier":       string(pkg.DepthTier),
		"budget_hmc":       pkg.BudgetHMC,
		"budget_runs":      pkg.BudgetRuns,
		"pool_distributed": pkg.PoolDistributed,
		"wasm_len":         len(raw),
	}, nil
}

func readWasmHex(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

func postSecurityAudit(base, adminTok string, payload map[string]any) ([]byte, int, error) {
	return postSecurityAuditAuth(base, payload)
}

// postSecurityAuditAuth tries node admin, loopback desktop admin, then developer token.
func postSecurityAuditAuth(base string, payload map[string]any) ([]byte, int, error) {
	body, _ := json.Marshal(payload)
	path := "/api/security-audit"
	if adm := adminToken(); adm != "" {
		b, code, err := apiDoAdmin(base, adm, http.MethodPost, path, body)
		if err != nil {
			return b, code, err
		}
		if code != http.StatusUnauthorized {
			return b, code, err
		}
	}
	if fuzzingcli.IsLoopbackBase(base) {
		if adm := fetchLoopbackAdminToken(base); adm != "" {
			b, code, err := apiDoAdmin(base, adm, http.MethodPost, path, body)
			if err != nil {
				return b, code, err
			}
			if code != http.StatusUnauthorized {
				return b, code, err
			}
		}
	}
	if dev := resolveToken(""); dev != "" {
		return apiDo(base, dev, http.MethodPost, path, body)
	}
	return nil, 0, fmt.Errorf("authentication required: set HACKME_ADMIN_TOKEN, use desktop node on loopback, or run: hackme-fuzzing register --save")
}

func fetchLoopbackAdminToken(base string) string {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/api/desktop/local-auth", nil)
	if err != nil {
		return ""
	}
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var j struct {
		AdminToken string `json:"admin_token"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&j); err != nil {
		return ""
	}
	return strings.TrimSpace(j.AdminToken)
}

func fetchGatePass(base, campaignID, reportTok string) (bool, error) {
	url := strings.TrimRight(base, "/") + "/api/fuzz/campaigns/" + campaignID + "/gate?max_critical=0&max_high=0"
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Hackme-Report-Token", reportTok)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("gate HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var j struct {
		Pass bool `json:"pass"`
	}
	if err := json.Unmarshal(b, &j); err != nil {
		return false, err
	}
	return j.Pass, nil
}

func marshalWizardSummary(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(bytes.TrimSpace(b), '\n'))
	return err
}
