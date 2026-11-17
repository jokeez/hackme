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
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzingcli"
	"hackme/internal/netutil"
)

func doWizard(base string, args []string) error {
	fs := flag.NewFlagSet("wizard", flag.ExitOnError)
	pkgName := fs.String("package", "", "B2B package: scan|audit|deep (default: pack default or audit)")
	packName := fs.String("pack", "", "ready detector pack: secrets|script_bounds|filter_utf8 (no custom rule needed)")
	title := fs.String("title", "", "campaign title")
	wasmPath := fs.String("wasm", "", "path to guard .wasm (optional when --pack is set)")
	payerRef := fs.String("payer-ref", "", "optional payer_ref for bookkeeping")
	publicProof := fs.Bool("public-proof", false, "publish Proof of Fuzz page + badge (no secret findings)")
	publicOK := fs.Bool("allow-public-base", false, "allow non-loopback base (not recommended)")
	where := fs.String("where", "", "pool (paid on chain, run by pool miners) | local (free, on this machine). Default: pool for audit/deep, local for scan")
	var seeds seedList
	fs.Var(&seeds, "seed", "example input file or directory of files for a check_bytes guard (repeatable; real packets/files make the fuzzer reach deep code)")
	_ = fs.Parse(args)

	if !*publicOK && !fuzzingcli.IsLoopbackBase(base) {
		return fmt.Errorf("wizard refuses non-loopback base %q (use local node or --allow-public-base)", base)
	}

	var pack fuzzingcli.GuardPack
	var havePack bool
	if strings.TrimSpace(*packName) != "" {
		p, err := fuzzingcli.GuardPackFor(*packName)
		if err != nil {
			return err
		}
		pack = p
		havePack = true
	}

	wasmFile := strings.TrimSpace(*wasmPath)
	if wasmFile == "" {
		if !havePack {
			return fmt.Errorf("usage: hackme-fuzzing wizard --pack secrets|script_bounds|filter_utf8 [--package audit]\n   or: hackme-fuzzing wizard --wasm guard.wasm [--package scan|audit|deep]")
		}
		root := findRepoRoot()
		resolved, err := fuzzingcli.ResolvePackWasm(root, pack)
		if err != nil {
			// try build
			if berr := buildPackWasm(root, pack); berr != nil {
				return fmt.Errorf("%v\nbuild: %v", err, berr)
			}
			resolved = filepath.Join(root, pack.WasmRelPath)
		}
		wasmFile = resolved
	}

	pkgKey := strings.TrimSpace(*pkgName)
	if pkgKey == "" {
		if havePack && pack.DefaultPackage != "" {
			pkgKey = pack.DefaultPackage
		} else {
			pkgKey = "audit"
		}
	}
	pkg, err := fuzzingcli.B2BPackageFor(pkgKey)
	if err != nil {
		return err
	}
	if havePack {
		pkg = fuzzingcli.AdjustPackageForPack(pkg, pack)
	}
	raw, err := os.ReadFile(wasmFile)
	if err != nil {
		return err
	}
	wasmHex := hex.EncodeToString(raw)
	pool := pkg.PoolDistributed
	createPoH := pkg.CreatePoHOrder

	campTitle := strings.TrimSpace(*title)
	if campTitle == "" {
		if havePack {
			campTitle = "HackMe Dig · " + pack.Title
		} else {
			campTitle = "HackMe Dig audit"
		}
	}

	payload := map[string]any{
		"title":            campTitle,
		"depth_tier":       string(pkg.DepthTier),
		"wasm_check_hex":   wasmHex,
		"budget_hmc":       pkg.BudgetHMC,
		"budget_runs":      pkg.BudgetRuns,
		"budget_seconds":   pkg.BudgetSeconds,
		"pool_distributed": pool,
		"create_poh_order": createPoH,
	}
	payload = fuzzingcli.ApplyDigEngineToPayload(payload, pkg)
	if havePack {
		cfg := fuzzingcli.ApplyPackConfig(map[string]any{}, pack)
		if v, ok := cfg["input_mode"]; ok {
			payload["input_mode"] = v
		}
		if v, ok := cfg["max_input_bytes"]; ok {
			payload["max_input_bytes"] = v
		}
		if v, ok := cfg["guided_scheduling"]; ok {
			payload["guided_scheduling"] = v
		}
		if v, ok := cfg["mutation_rounds"]; ok {
			payload["mutation_rounds"] = v
		}
		if v, ok := cfg["seed_byte_corpus"]; ok {
			payload["seed_byte_corpus"] = v
		}
		if v, ok := cfg["seed_corpus"]; ok {
			payload["seed_corpus"] = v
		}
		payload["guard_pack"] = pack.ID
		payload["guard_name"] = pack.ID
		// Bytes packs pair naturally with deep tier signals; keep customer package budget.
		if pack.InputMode == "bytes" && pkg.DepthTier == fuzzengine.DepthWasmOnly {
			payload["depth_tier"] = string(fuzzengine.DepthBytesCorpus)
		} else if pack.InputMode == "bytes" {
			payload["depth_tier"] = string(fuzzengine.DepthBytesCorpus)
		}
	}
	if pkg.RewardHMC > 0 {
		payload["reward_hmc"] = pkg.RewardHMC
	}
	if pr := strings.TrimSpace(*payerRef); pr != "" {
		payload["payer_ref"] = pr
	}
	if *publicProof {
		payload["public_proof"] = true
	}
	if len(seeds) > 0 {
		hexSeeds, err := readSeedInputs(seeds)
		if err != nil {
			return err
		}
		payload["seed_byte_corpus"] = hexSeeds
		payload["input_mode"] = "bytes" // seeds are byte inputs: the guard must be check_bytes
	}
	mode := strings.ToLower(strings.TrimSpace(*where))
	if mode == "" {
		if pkg.Name == "audit" || pkg.Name == "deep" {
			mode = "pool"
		} else {
			mode = "local"
		}
	}
	if mode == "pool" {
		order := map[string]any{"dig_package": pkg.Name, "title": campTitle}
		if havePack && strings.TrimSpace(*wasmPath) == "" {
			order["guard_pack"] = pack.ID
		} else {
			order["wasm_check_hex"] = wasmHex
			if havePack {
				order["guard_pack"] = pack.ID
			}
		}
		if v, ok := payload["seed_byte_corpus"]; ok {
			order["seed_byte_corpus"] = v
		}
		done, err := runPoolOrder(base, order)
		if done || err != nil {
			return err
		}
		fmt.Println("The network does not take paid pool orders yet — running the scan on this machine instead.")
		payload["pool_distributed"] = false
	} else {
		payload["pool_distributed"] = false
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
	// Campaign data lives on --base (loopback). Default report/gate/pulse to that node.
	// Override only via HACKME_PUBLIC_REPORT_BASE when reports are mirrored to a public host.
	publicBase := strings.TrimSpace(os.Getenv("HACKME_PUBLIC_REPORT_BASE"))
	if publicBase == "" {
		publicBase = strings.TrimRight(base, "/")
	}
	publicBase = strings.TrimRight(publicBase, "/")
	reportURL := publicBase + "/api/fuzz/campaigns/" + campaignID + "/report.html"
	gateURL := publicBase + "/api/fuzz/campaigns/" + campaignID + "/gate?max_critical=0&max_high=0"
	pulseURL := publicBase + "/api/fuzz/campaigns/" + campaignID + "/pulse"

	billing := fuzzingcli.QuoteDigBilling(pkg)
	out := map[string]any{
		"ok":                    true,
		"package":               pkg.Name,
		"product_sku":           fuzzingcli.B2BPackageDisplayName(pkg.Name),
		"package_summary":       pkg.Summary,
		"signal_types":          pkg.SignalTypes,
		"depth_tier":            payload["depth_tier"],
		"product_mode":          billing.ProductMode,
		"dig_billing":           billing,
		"budget_hmc":            pkg.BudgetHMC,
		"budget_runs":           pkg.BudgetRuns,
		"budget_seconds":        pkg.BudgetSeconds,
		"pool_distributed":      pool,
		"order_id":              orderID,
		"campaign_id":           campaignID,
		"customer_report_token": reportTok,
		"primary_deliverable":   "gate",
		"gate_url":              gateURL,
		"report_url":            reportURL,
		"pulse_url":             pulseURL,
		"ci_header":             "X-Hackme-Report-Token",
		"status_hint":           statusHint(campaignID, orderID),
	}
	if havePack {
		out["pack"] = pack.ID
		out["pack_title"] = pack.Title
		out["pack_summary"] = pack.Summary
		out["input_mode"] = pack.InputMode
		out["explain_example"] = fuzzingcli.ExplainPackFinding(pack.ID, explainSample(pack), "")
	}
	if *publicProof {
		out["public_proof"] = true
		out["proof_url"] = publicBase + "/proof/" + campaignID
		out["badge_url"] = publicBase + "/proof/" + campaignID + "/badge.svg"
	}
	if v, ok := resp["proof_url"].(string); ok && strings.TrimSpace(v) != "" {
		if strings.HasPrefix(v, "http") {
			out["proof_url"] = v
		} else {
			out["proof_url"] = publicBase + v
		}
	}
	if v, ok := resp["badge_url"].(string); ok && strings.TrimSpace(v) != "" {
		if strings.HasPrefix(v, "http") {
			out["badge_url"] = v
		} else {
			out["badge_url"] = publicBase + v
		}
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
	if havePack {
		fmt.Fprintln(os.Stderr, "Pack:", pack.ID, "—", pack.Title)
		fmt.Fprintln(os.Stderr, "     ", pack.Summary)
		fmt.Fprintln(os.Stderr, "Explain sample:", fuzzingcli.ExplainPackFinding(pack.ID, explainSample(pack), ""))
		fmt.Fprintln(os.Stderr, "")
	}
	if u, ok := out["proof_url"].(string); ok && u != "" {
		fmt.Fprintln(os.Stderr, "Proof of Fuzz (public facts):", u)
		if b, ok := out["badge_url"].(string); ok {
			fmt.Fprintln(os.Stderr, "Badge SVG:", b)
		}
		fmt.Fprintln(os.Stderr, "")
	}
	fmt.Fprintln(os.Stderr, "Package:", pkg.Name, "—", pkg.Summary)
	fmt.Fprintln(os.Stderr, "Signals:", strings.Join(pkg.SignalTypes, ", "))
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Primary deliverable — CI gate (pass/fail):")
	fmt.Fprintf(os.Stderr, "  curl -sS -H \"X-Hackme-Report-Token: <save-token>\" '%s'\n", gateURL)
	fmt.Fprintln(os.Stderr, "  # jq -e '.pass == true'  → exit 0 on CLEAN gate")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Next steps:")
	fmt.Fprintln(os.Stderr, "  1. Save report token securely (shown once in JSON).")
	fmt.Fprintf(os.Stderr, "  2. Honest status:  %s\n", statusHint(campaignID, orderID))
	fmt.Fprintf(os.Stderr, "  3. Open report:    %s\n", reportURL)
	fmt.Fprintf(os.Stderr, "  4. Watch pulse:    curl -H \"X-Hackme-Report-Token: …\" %s\n", pulseURL)
	return nil
}

func explainSample(p fuzzingcli.GuardPack) string {
	if len(p.SeedByteCorpus) > 0 {
		switch s := p.SeedByteCorpus[0].(type) {
		case string:
			if p.ID == "filter_utf8" {
				return "\xc7="
			}
			return s
		}
	}
	return p.Title
}

func findRepoRoot() string {
	wd, _ := os.Getwd()
	dir := wd
	for i := 0; i < 10; i++ {
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
	return wd
}

func buildPackWasm(root string, p fuzzingcli.GuardPack) error {
	src := filepath.Join(root, p.SourceRelPath)
	out := filepath.Join(root, p.WasmRelPath)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	cmd := exec.Command("rustc", "--target", "wasm32-unknown-unknown", "-O", "--crate-type=cdylib", src, "-o", out)
	cmd.Dir = root
	b, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(b)))
	}
	return nil
}

// doWizardDryRun builds the security-audit payload without calling the API (for tests).
func doWizardDryRun(pkgName, wasmPath string) (map[string]any, error) {
	return doWizardDryRunPack(pkgName, "", wasmPath)
}

func doWizardDryRunPack(pkgName, packName, wasmPath string) (map[string]any, error) {
	var pack fuzzingcli.GuardPack
	havePack := false
	if strings.TrimSpace(packName) != "" {
		p, err := fuzzingcli.GuardPackFor(packName)
		if err != nil {
			return nil, err
		}
		pack = p
		havePack = true
		if strings.TrimSpace(pkgName) == "" {
			pkgName = pack.DefaultPackage
		}
		if strings.TrimSpace(wasmPath) == "" {
			root := findRepoRoot()
			_ = buildPackWasm(root, pack)
			wasmPath = filepath.Join(root, pack.WasmRelPath)
		}
	}
	if strings.TrimSpace(pkgName) == "" {
		pkgName = "audit"
	}
	pkg, err := fuzzingcli.B2BPackageFor(pkgName)
	if err != nil {
		return nil, err
	}
	if havePack {
		pkg = fuzzingcli.AdjustPackageForPack(pkg, pack)
	}
	raw, err := os.ReadFile(wasmPath)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"depth_tier":       string(pkg.DepthTier),
		"budget_hmc":       pkg.BudgetHMC,
		"budget_runs":      pkg.BudgetRuns,
		"budget_seconds":   pkg.BudgetSeconds,
		"pool_distributed": pkg.PoolDistributed,
		"create_poh_order": pkg.CreatePoHOrder,
		"signal_types":     pkg.SignalTypes,
		"summary":          pkg.Summary,
		"wasm_len":         len(raw),
	}
	out = fuzzingcli.ApplyDigEngineToPayload(out, pkg)
	if havePack {
		cfg := fuzzingcli.ApplyPackConfig(nil, pack)
		out["pack"] = pack.ID
		out["input_mode"] = cfg["input_mode"]
		out["guard_pack"] = pack.ID
		out["guided_scheduling"] = cfg["guided_scheduling"]
		if pack.InputMode == "bytes" {
			out["depth_tier"] = string(fuzzengine.DepthBytesCorpus)
		}
		if mr, ok := cfg["mutation_rounds"]; ok {
			out["mutation_rounds"] = mr
		}
	}
	return out, nil
}

func statusHint(campaignID, orderID string) string {
	s := "hackme-fuzzing status --campaign " + campaignID
	if orderID != "" {
		s += " --order " + orderID
	}
	s += " --report-token <token>"
	return s
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

// postSecurityAuditAuth tries node admin, then loopback desktop local-auth.
// Developer/integrator tokens cannot create paid security-audit escrow (admin-strict API).
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
	return nil, 0, fmt.Errorf("authentication required: set HACKME_ADMIN_TOKEN or use a desktop node on loopback (GET /api/desktop/local-auth)")
}

func fetchLoopbackAdminToken(base string) string {
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/api/desktop/local-auth", nil)
	if err != nil {
		return ""
	}
	if req.URL.User != nil && req.URL.User.String() != "" {
		return ""
	}
	// Fail-closed: do not spoof Sec-Fetch-Site. CLI gets admin_token only when the node
	// has HACKME_DESKTOP_EXPOSE_ADMIN_TOKEN=1 (or set HACKME_ADMIN_TOKEN / token file).
	resp, err := netutil.HTTPClientNoRedirect(5 * time.Second).Do(req)
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
	if req.URL.User != nil && req.URL.User.String() != "" {
		return false, fmt.Errorf("refusing request: URL userinfo not allowed on API base")
	}
	req.Header.Set("X-Hackme-Report-Token", reportTok)
	// Do not attach HACKME_ADMIN_TOKEN — gate is report-token scoped (mirror fetchJSONAuth).
	resp, err := netutil.HTTPClientNoRedirect(30 * time.Second).Do(req)
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

type seedList []string

func (s *seedList) String() string     { return strings.Join(*s, ",") }
func (s *seedList) Set(v string) error { *s = append(*s, v); return nil }

// readSeedInputs loads --seed files (a directory contributes every regular file in it) as
// hex strings. Limits match the sandbox: 4096 bytes per input, at most 256 inputs.
func readSeedInputs(paths []string) ([]string, error) {
	const maxSeed, maxCount = 4096, 256
	var out []string
	add := func(p string) error {
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if len(b) == 0 {
			return nil
		}
		if len(b) > maxSeed {
			b = b[:maxSeed]
		}
		if len(out) < maxCount {
			out = append(out, hex.EncodeToString(b))
		}
		return nil
	}
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			return nil, fmt.Errorf("--seed %s: %w", p, err)
		}
		if !st.IsDir() {
			if err := add(p); err != nil {
				return nil, err
			}
			continue
		}
		ents, err := os.ReadDir(p)
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			if e.Type().IsRegular() {
				if err := add(filepath.Join(p, e.Name())); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("--seed: no non-empty input files found")
	}
	return out, nil
}

// runPoolOrder pays for the scan on chain and puts it on the public pool via the local node.
// done=false (and no error) means the network does not accept paid pool orders yet.
func runPoolOrder(base string, order map[string]any) (bool, error) {
	adm := adminToken()
	if adm == "" && fuzzingcli.IsLoopbackBase(base) {
		adm = fetchLoopbackAdminToken(base)
	}
	if adm == "" {
		return false, fmt.Errorf("admin token required (run on the Orders node, or set HACKME_ADMIN_TOKEN)")
	}
	body, _ := json.Marshal(order)
	fmt.Println("Paying for the scan on chain and sending it to the pool — this waits for one block (~30 s)…")
	b, code, err := apiDoAdmin(base, adm, http.MethodPost, "/api/orders/pool-order", body)
	if err != nil {
		return false, err
	}
	var resp map[string]any
	_ = json.Unmarshal(b, &resp)
	if code == http.StatusConflict {
		if c, _ := resp["code"].(string); c == "pool_orders_unavailable" || c == "no_canonical_chain" {
			return false, nil
		}
	}
	if code != http.StatusOK {
		return true, fmt.Errorf("pool order HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	str := func(k string) string { v, _ := resp[k].(string); return v }
	num := func(k string) float64 { v, _ := resp[k].(float64); return v }
	fmt.Println()
	fmt.Printf("Pool order %s\n", str("campaign_id"))
	fmt.Printf("  paid:        %.8f HMC on chain (tx %s)\n", num("funded_hmc"), str("funding_tx_hash"))
	fmt.Printf("  how it pays: %s\n", str("payment"))
	fmt.Printf("  report:      %s\n", str("report_url"))
	fmt.Printf("  CI gate:     %s\n", str("gate_url"))
	if tok := str("customer_report_token"); tok != "" {
		fmt.Printf("  report token (save it — shown once): %s\n", tok)
		fmt.Printf("  curl -sS -H \"X-Hackme-Report-Token: %s\" '%s'\n", tok, str("gate_url"))
	}
	return true, nil
}
