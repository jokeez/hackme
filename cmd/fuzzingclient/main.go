// Command fuzzingclient (hackme-fuzzing) — B2B fuzzing: token self-service, local WASM build, orders.
package main

import (
	"bytes"
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

	"hackme/internal/fuzzingcli"
)

func main() {
	defaultBase := envOr("HACKME_FUZZING_BASE", "http://127.0.0.1:8080")
	baseFlag := flag.String("base", defaultBase, "local node API base (integrators: loopback, not hackme.tech)")
	tokenFlag := flag.String("token", "", "developer token (or load from config file)")
	saveFlag := flag.Bool("save", false, "with register/rotate: save token to config file")
	flag.Parse()
	base, token, save, cmd, rest := reconcileCLI(*baseFlag, *tokenFlag, *saveFlag, flag.Args())
	if cmd == "" {
		usage()
		os.Exit(2)
	}
	tok := resolveToken(token)
	saveTok := wantsSave(save, rest...)
	switch cmd {
	case "register":
		if err := doRegister(base, saveTok); err != nil {
			fail(err)
		}
	case "rotate":
		if tok == "" {
			failMsg("token required: hackme-fuzzing rotate (or set HACKME_DEVELOPER_TOKEN / run register --save)")
		}
		if err := doRotate(base, tok, saveTok); err != nil {
			fail(err)
		}
	case "wallet":
		if tok == "" {
			failMsg("token required for wallet")
		}
		if err := doWallet(base, tok); err != nil {
			fail(err)
		}
	case "tasks", "list":
		if tok == "" {
			failMsg("token required for tasks")
		}
		if err := doTasks(base, tok); err != nil {
			fail(err)
		}
	case "create":
		if tok == "" {
			failMsg("token required for create")
		}
		manifest := firstPositionalArg(rest)
		if manifest == "" {
			failMsg("usage: hackme-fuzzing create manifest.json")
		}
		if err := doCreate(base, tok, manifest); err != nil {
			fail(err)
		}
	case "build":
		if err := delegateBuild(rest); err != nil {
			fail(err)
		}
	case "campaign":
		if err := doCampaign(base, rest); err != nil {
			fail(err)
		}
	case "wizard":
		if err := doWizard(base, rest); err != nil {
			fail(err)
		}
	case "status":
		if err := doStatus(base, rest); err != nil {
			fail(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `hackme-fuzzing — B2B fuzzing integrator CLI

  hackme-fuzzing register [--save]     # auto-issue developer token (POST /api/integrator/register)
  hackme-fuzzing rotate [--save]       # rotate token (invalidates old)
  hackme-fuzzing build -lang rust -source check.rs   (runs hackme-fuzzing-build beside this binary)
  hackme-fuzzing wallet
  hackme-fuzzing create manifest.json
  hackme-fuzzing tasks
  hackme-fuzzing campaign create -title "..." -runs 200 [-task-id ORDER]
  hackme-fuzzing campaign status CAMPAIGN_ID
  hackme-fuzzing campaign report-url CAMPAIGN_ID
  hackme-fuzzing wizard --wasm guard.wasm [--package scan|audit|deep] [-title "..."]
  hackme-fuzzing status --campaign ID [--order ORDER_ID] [--report-token TOKEN]

  Happy path: register --save → wizard --package audit → status → gate/report URLs
  Packages: scan=WASM smoke · audit=WASM+native/ASAN · deep=byte corpus (hours-scale)
  Primary deliverable: CI gate pass/fail (not finding spam)

Env: HACKME_FUZZING_BASE, HACKME_DEVELOPER_TOKEN, HACKME_REPORT_TOKEN
Campaign admin: HACKME_ADMIN_TOKEN (create/status/wizard on local node)
Public report base: HACKME_PUBLIC_REPORT_BASE (default https://hackme.tech)
Config: %s
`, fuzzingcli.TokenConfigPath())
}

// wantsSave accepts --save before or after the subcommand (Go flags stop at first positional).
func wantsSave(flagVal bool, args ...string) bool {
	if flagVal {
		return true
	}
	for _, a := range args {
		if a == "--save" {
			return true
		}
	}
	return false
}

// reconcileCLI merges global flags from before and after the subcommand.
// Go's flag package stops at the first positional, so `hackme-fuzzing tasks --base URL` must work.
func reconcileCLI(base, token string, save bool, args []string) (string, string, bool, string, []string) {
	if len(args) == 0 {
		return base, token, save, "", nil
	}
	cmd := args[0]
	rest := pullGlobalFlags(args[1:], &base, &token, &save)
	return base, token, save, cmd, rest
}

func pullGlobalFlags(args []string, base, token *string, save *bool) []string {
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--base" && i+1 < len(args):
			*base = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--base="):
			*base = strings.TrimSpace(strings.TrimPrefix(a, "--base="))
		case a == "--token" && i+1 < len(args):
			*token = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(a, "--token="):
			*token = strings.TrimSpace(strings.TrimPrefix(a, "--token="))
		case a == "--save":
			*save = true
		default:
			out = append(out, a)
		}
	}
	return out
}

func firstPositionalArg(args []string) string {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		return a
	}
	return ""
}

func envOr(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	if k == "HACKME_FUZZING_BASE" {
		if v := strings.TrimSpace(os.Getenv("HACKME_PHASING_BASE")); v != "" {
			return v
		}
	}
	return def
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}

func failMsg(msg string) {
	fmt.Fprintln(os.Stderr, "error:", msg)
	os.Exit(2)
}

func resolveToken(flagTok string) string {
	if t := strings.TrimSpace(flagTok); t != "" {
		return t
	}
	if t := strings.TrimSpace(os.Getenv("HACKME_DEVELOPER_TOKEN")); t != "" {
		return t
	}
	b, err := os.ReadFile(fuzzingcli.TokenConfigPath())
	if err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

func saveToken(tok string) error {
	p := fuzzingcli.TokenConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(tok+"\n"), 0o600)
}

func adminToken() string {
	return strings.TrimSpace(os.Getenv("HACKME_ADMIN_TOKEN"))
}

func apiDoAdmin(base, adminTok, method, path string, body []byte) ([]byte, int, error) {
	url := strings.TrimRight(base, "/") + path
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return nil, 0, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if adminTok != "" {
		req.Header.Set("X-Hackme-Admin-Token", adminTok)
	}
	cl := &http.Client{Timeout: 90 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return b, resp.StatusCode, err
}

func apiDo(base, token, method, path string, body []byte) ([]byte, int, error) {
	url := strings.TrimRight(base, "/") + path
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return nil, 0, err
	}
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("X-Hackme-Developer-Token", token)
	}
	cl := &http.Client{Timeout: 60 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return b, resp.StatusCode, err
}

func doRegister(base string, save bool) error {
	body, _ := json.Marshal(map[string]string{"label": hostnameLabel()})
	b, code, err := apiDo(base, "", http.MethodPost, "/api/integrator/register", body)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("register HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	tok, _ := out["developer_token"].(string)
	if tok == "" {
		return fmt.Errorf("no developer_token in response")
	}
	printJSON(out)
	if save {
		if err := saveToken(tok); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "saved token to %s\n", fuzzingcli.TokenConfigPath())
	} else {
		fmt.Fprintln(os.Stderr, "tip: re-run with --save or export HACKME_DEVELOPER_TOKEN=…")
	}
	return nil
}

func doRotate(base, old string, save bool) error {
	b, code, err := apiDo(base, old, http.MethodPost, "/api/integrator/rotate", nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("rotate HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	tok, _ := out["developer_token"].(string)
	printJSON(out)
	if save && tok != "" {
		_ = saveToken(tok)
		fmt.Fprintf(os.Stderr, "saved new token to %s\n", fuzzingcli.TokenConfigPath())
	}
	return nil
}

func hostnameLabel() string {
	h, _ := os.Hostname()
	h = strings.TrimSpace(h)
	if h == "" {
		return "cli-integrator"
	}
	return h
}

func doWallet(base, token string) error {
	b, code, err := apiDo(base, token, http.MethodGet, "/api/wallet", nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("GET /api/wallet HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	var j map[string]any
	_ = json.Unmarshal(b, &j)
	if j["public_redacted"] == true {
		fmt.Println("Billing: network operator escrow (no public treasury address).")
		if note, _ := j["note"].(string); note != "" {
			fmt.Println(note)
		}
		fmt.Println("Track orders: hackme-fuzzing tasks · payer_ref in your manifest.")
		return nil
	}
	fmt.Println(string(prettyJSON(b)))
	return nil
}

func doTasks(base, token string) error {
	b, code, err := apiDo(base, token, http.MethodGet, "/api/tasks", nil)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("GET /api/tasks HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	fmt.Println(string(prettyJSON(b)))
	return nil
}

func doCreate(base, token, manifestPath string) error {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	b, code, err := apiDo(base, token, http.MethodPost, "/api/tasks", raw)
	if err != nil {
		return err
	}
	if code != http.StatusOK {
		return fmt.Errorf("POST /api/tasks HTTP %d: %s", code, strings.TrimSpace(string(b)))
	}
	fmt.Println(string(prettyJSON(b)))
	return nil
}

func delegateBuild(args []string) error {
	helper, err := locateBuildHelper()
	if err != nil {
		return err
	}
	cmd := exec.Command(helper, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return err
	}
	return nil
}

func locateBuildHelper() (string, error) {
	if v := strings.TrimSpace(os.Getenv("HACKME_FUZZING_BUILD")); v != "" {
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return v, nil
		}
		return "", fmt.Errorf("HACKME_FUZZING_BUILD not found: %s", v)
	}
	for _, c := range fuzzingcli.BuildHelperCandidates() {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("hackme-fuzzing-build not found beside %s — download from hackme.tech/downloads.html#fuzzing-client or set HACKME_FUZZING_BUILD", mustExecutable())
}

func mustExecutable() string {
	exe, err := os.Executable()
	if err != nil {
		return "hackme-fuzzing"
	}
	return exe
}

func prettyJSON(b []byte) []byte {
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return b
	}
	return buf.Bytes()
}

func printJSON(v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(b))
}

func doCampaign(base string, args []string) error {
	adm := adminToken()
	if adm == "" {
		return fmt.Errorf("HACKME_ADMIN_TOKEN required for campaign commands (local node admin)")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: hackme-fuzzing campaign create|status|report-url ...")
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("campaign-create", flag.ExitOnError)
		title := fs.String("title", "property-fuzz-v2", "campaign title")
		runs := fs.Int("runs", 120, "budget_runs")
		budgetHMC := fs.Float64("budget-hmc", 0, "escrow budget HMC (20%% runs / 80%% bounty)")
		pool := fs.Bool("pool", false, "register on pool coordinator (pool_distributed)")
		taskID := fs.String("task-id", "", "linked order task id")
		wasmHex := fs.String("wasm-hex", "", "inline wasm_check_hex")
		ctype := fs.String("type", "property", "campaign_type")
		depthTier := fs.String("depth-tier", "", "depth tier: wasm_only|wasm_native|bytes_corpus")
		_ = fs.Parse(args[1:])
		cfg := map[string]any{
			"fuzz_engine_version": "fuzz_engine_v2",
			"mutation_rounds":     4,
			"coverage_guided":     true,
		}
		if dt := strings.TrimSpace(*depthTier); dt != "" {
			cfg["depth_tier"] = dt
		}
		if *pool {
			cfg["pool_distributed"] = true
			cfg["auto_runner"] = "0"
		}
		if *wasmHex != "" {
			cfg["wasm_check_hex"] = *wasmHex
		}
		payload := map[string]any{
			"campaign_type":  *ctype,
			"title":          *title,
			"description":    "HackMe fuzz engine v2 — seed corpus + mutation + coverage buckets",
			"budget_runs":    *runs,
			"budget_seconds": 3600,
			"task_id":        *taskID,
			"config":         cfg,
		}
		if *budgetHMC > 0 {
			payload["budget_hmc"] = *budgetHMC
		}
		body, _ := json.Marshal(payload)
		b, code, err := apiDoAdmin(base, adm, http.MethodPost, "/api/fuzz/campaigns", body)
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("POST /api/fuzz/campaigns HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	case "status":
		if len(args) < 2 {
			return fmt.Errorf("usage: hackme-fuzzing campaign status CAMPAIGN_ID")
		}
		id := args[1]
		b, code, err := apiDoAdmin(base, adm, http.MethodGet, "/api/fuzz/campaigns/"+id, nil)
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("GET campaign HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	case "report-url":
		if len(args) < 2 {
			return fmt.Errorf("usage: hackme-fuzzing campaign report-url CAMPAIGN_ID")
		}
		id := args[1]
		fmt.Printf("%s/api/fuzz/campaigns/%s/report.html\n", strings.TrimRight(base, "/"), id)
		fmt.Fprintf(os.Stderr, "Pass X-Hackme-Report-Token from create response to view.\n")
		return nil
	default:
		return fmt.Errorf("unknown campaign subcommand %q", args[0])
	}
}
