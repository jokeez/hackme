// Command fuzzingclient (hackme-fuzzing) — B2B fuzzing: token self-service, local WASM build, orders.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/taskbuild"
)

func main() {
	base := flag.String("base", envOr("HACKME_FUZZING_BASE", "https://hackme.tech"), "API base URL")
	token := flag.String("token", "", "developer token (or load from config file)")
	save := flag.Bool("save", false, "with register/rotate: save token to config file")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	tok := resolveToken(*token)
	cmd := args[0]
	rest := args[1:]
	switch cmd {
	case "register":
		if err := doRegister(*base, *save); err != nil {
			fail(err)
		}
	case "rotate":
		if tok == "" {
			failMsg("token required: hackme-fuzzing rotate (or set HACKME_DEVELOPER_TOKEN / run register --save)")
		}
		if err := doRotate(*base, tok, *save); err != nil {
			fail(err)
		}
	case "wallet":
		if tok == "" {
			failMsg("token required for wallet")
		}
		if err := doWallet(*base, tok); err != nil {
			fail(err)
		}
	case "tasks", "list":
		if tok == "" {
			failMsg("token required for tasks")
		}
		if err := doTasks(*base, tok); err != nil {
			fail(err)
		}
	case "create":
		if tok == "" {
			failMsg("token required for create")
		}
		if len(rest) < 1 {
			failMsg("usage: hackme-fuzzing create manifest.json")
		}
		if err := doCreate(*base, tok, rest[0]); err != nil {
			fail(err)
		}
	case "build":
		if err := doBuild(rest); err != nil {
			fail(err)
		}
	case "campaign":
		if err := doCampaign(*base, rest); err != nil {
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
  hackme-fuzzing build -lang rust -source check.rs [-out dir] [-id name] ...
  hackme-fuzzing wallet
  hackme-fuzzing create manifest.json
  hackme-fuzzing tasks
  hackme-fuzzing campaign create -title "..." -runs 200 [-task-id ORDER]
  hackme-fuzzing campaign status CAMPAIGN_ID
  hackme-fuzzing campaign report-url CAMPAIGN_ID

Env: HACKME_FUZZING_BASE, HACKME_DEVELOPER_TOKEN
Campaign admin: HACKME_ADMIN_TOKEN (create/status on local node)
Config: %s
`, tokenConfigPath())
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

func tokenConfigPath() string {
	if v := strings.TrimSpace(os.Getenv("HACKME_DEVELOPER_TOKEN_FILE")); v != "" {
		return v
	}
	if h := os.Getenv("XDG_CONFIG_HOME"); h != "" {
		return filepath.Join(h, "hackme", "developer.token")
	}
	if home, _ := os.UserHomeDir(); home != "" {
		return filepath.Join(home, ".config", "hackme", "developer.token")
	}
	return "developer.token"
}

func resolveToken(flagTok string) string {
	if t := strings.TrimSpace(flagTok); t != "" {
		return t
	}
	if t := strings.TrimSpace(os.Getenv("HACKME_DEVELOPER_TOKEN")); t != "" {
		return t
	}
	b, err := os.ReadFile(tokenConfigPath())
	if err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

func saveToken(tok string) error {
	p := tokenConfigPath()
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
		fmt.Fprintf(os.Stderr, "saved token to %s\n", tokenConfigPath())
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
		fmt.Fprintf(os.Stderr, "saved new token to %s\n", tokenConfigPath())
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

func doBuild(args []string) error {
	fs := flag.NewFlagSet("build", flag.ExitOnError)
	lang := fs.String("lang", "rust", "language")
	source := fs.String("source", "", "source file path")
	out := fs.String("out", "fuzzing-out", "output directory")
	id := fs.String("id", "", "order id")
	reward := fs.Float64("reward", 0.01, "reward_hmc per solve")
	diff := fs.Int("difficulty", 5, "difficulty_score")
	target := fs.Int("target", 3, "target_solves")
	payer := fs.String("payer-ref", "", "payer_ref")
	_ = fs.Parse(args)
	if *source == "" {
		return fmt.Errorf("build requires -source file")
	}
	code, err := os.ReadFile(*source)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	res, err := taskbuild.BuildFromSource(ctx, taskbuild.Options{
		ID:              *id,
		Language:        *lang,
		Source:          string(code),
		RewardHMC:       *reward,
		DifficultyScore: *diff,
		TargetSolves:    *target,
		PayerRef:        *payer,
		OutDir:          *out,
	})
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wasm: %s (%d bytes) sha256=%s\n", res.WasmPath, len(res.WasmBytes), res.ArtifactHash)
	fmt.Println(string(res.ManifestJSON))
	return nil
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
		taskID := fs.String("task-id", "", "linked order task id")
		wasmHex := fs.String("wasm-hex", "", "inline wasm_check_hex")
		ctype := fs.String("type", "property", "campaign_type")
		_ = fs.Parse(args[1:])
		cfg := map[string]any{
			"fuzz_engine_version": "fuzz_engine_v2",
			"mutation_rounds":     4,
			"coverage_guided":     true,
		}
		if *wasmHex != "" {
			cfg["wasm_check_hex"] = *wasmHex
		}
		body, _ := json.Marshal(map[string]any{
			"campaign_type": *ctype,
			"title":         *title,
			"description":   "HackMe fuzz engine v2 — seed corpus + mutation + coverage buckets",
			"budget_runs":   *runs,
			"budget_seconds": 3600,
			"task_id":       *taskID,
			"config":        cfg,
		})
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
