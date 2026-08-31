package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
)

func doHunt(base string, args []string) error {
	adm := adminToken()
	if adm == "" {
		return fmt.Errorf("HACKME_ADMIN_TOKEN required for hunt commands")
	}
	if len(args) < 1 {
		return fmt.Errorf("usage: hackme-fuzzing hunt pin|inventory|template|build|create|packages|targets ...")
	}
	switch args[0] {
	case "packages":
		b, code, err := apiDoAdmin(base, adm, http.MethodGet, "/api/hunt/packages", nil)
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("GET /api/hunt/packages HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	case "targets":
		b, code, err := apiDoAdmin(base, adm, http.MethodGet, "/api/hunt/targets", nil)
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("GET /api/hunt/targets HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	case "pin":
		fs := flag.NewFlagSet("hunt-pin", flag.ExitOnError)
		path := fs.String("path", "", "local repo path")
		gitURL := fs.String("git-url", "", "https:// or git@ clone URL")
		ref := fs.String("ref", "main", "git ref")
		_ = fs.Parse(args[1:])
		body, _ := json.Marshal(map[string]string{
			"path":    strings.TrimSpace(*path),
			"git_url": strings.TrimSpace(*gitURL),
			"ref":     strings.TrimSpace(*ref),
		})
		b, code, err := apiDoAdmin(base, adm, http.MethodPost, "/api/hunt/repo/pin", body)
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("POST /api/hunt/repo/pin HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	case "inventory":
		fs := flag.NewFlagSet("hunt-inventory", flag.ExitOnError)
		path := fs.String("path", "", "repo root to scan")
		maxFiles := fs.Int("max-files", 400, "max source files")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(*path) == "" {
			return fmt.Errorf("hunt inventory requires --path")
		}
		body, _ := json.Marshal(map[string]any{
			"path":      strings.TrimSpace(*path),
			"max_files": *maxFiles,
		})
		b, code, err := apiDoAdmin(base, adm, http.MethodPost, "/api/hunt/inventory", body)
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("POST /api/hunt/inventory HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	case "template", "template-preview":
		fs := flag.NewFlagSet("hunt-template", flag.ExitOnError)
		pinPath := fs.String("pin-path", "", "pinned repo path from hunt pin")
		sourceRel := fs.String("source", "", "source file relative to pin")
		repoPath := fs.String("repo-path", "", "pin local path inline")
		gitURL := fs.String("git-url", "", "pin git URL inline")
		ref := fs.String("ref", "main", "git ref")
		_ = fs.Parse(args[1:])
		payload := map[string]any{
			"pin_path":   strings.TrimSpace(*pinPath),
			"source_rel": strings.TrimSpace(*sourceRel),
		}
		if strings.TrimSpace(*repoPath) != "" || strings.TrimSpace(*gitURL) != "" {
			payload["repo"] = map[string]string{
				"path":    strings.TrimSpace(*repoPath),
				"git_url": strings.TrimSpace(*gitURL),
				"ref":     strings.TrimSpace(*ref),
			}
		}
		if payload["source_rel"] == "" {
			return fmt.Errorf("hunt template requires --source")
		}
		b, code, err := apiDoAdmin(base, adm, http.MethodPost, "/api/hunt/template/preview", mustJSON(payload))
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("POST /api/hunt/template/preview HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	case "build":
		fs := flag.NewFlagSet("hunt-build", flag.ExitOnError)
		sourceRel := fs.String("source", "", "inventory source relative path")
		repoPath := fs.String("repo-path", "", "pinned local path")
		gitURL := fs.String("git-url", "", "git clone URL")
		ref := fs.String("ref", "main", "git ref")
		templateAccept := fs.Bool("template-accept", false, "wrap stdin driver when LLVMFuzzerTestOneInput missing")
		_ = fs.Parse(args[1:])
		if strings.TrimSpace(*sourceRel) == "" {
			return fmt.Errorf("hunt build requires --source")
		}
		payload := map[string]any{
			"source_rel":      strings.TrimSpace(*sourceRel),
			"template_accept": *templateAccept,
		}
		if strings.TrimSpace(*repoPath) != "" || strings.TrimSpace(*gitURL) != "" {
			payload["repo"] = map[string]string{
				"path":    strings.TrimSpace(*repoPath),
				"git_url": strings.TrimSpace(*gitURL),
				"ref":     strings.TrimSpace(*ref),
			}
		}
		b, code, err := apiDoAdmin(base, adm, http.MethodPost, "/api/hunt/harness/build", mustJSON(payload))
		if err != nil {
			return err
		}
		if code != http.StatusOK {
			return fmt.Errorf("POST /api/hunt/harness/build HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	case "create":
		fs := flag.NewFlagSet("hunt-create", flag.ExitOnError)
		pkg := fs.String("package", "hunt_lite", "hunt_lite|hunt_standard")
		title := fs.String("title", "", "campaign title")
		targetID := fs.String("target", "", "catalog target id")
		sourceRel := fs.String("source", "", "inventory source relative path")
		repoPath := fs.String("repo-path", "", "local repo for inventory")
		gitURL := fs.String("git-url", "", "git repo for inventory")
		ref := fs.String("ref", "main", "git ref")
		budgetHMC := fs.Float64("budget-hmc", 0, "override budget HMC")
		pool := fs.Bool("pool", true, "pool distributed shards")
		templateAccept := fs.Bool("template-accept", false, "template Accept for inventory build")
		status := fs.String("status", "running", "planned|running")
		_ = fs.Parse(args[1:])
		payload := map[string]any{
			"package":           strings.TrimSpace(*pkg),
			"title":             strings.TrimSpace(*title),
			"pool_distributed":  *pool,
			"template_accept":   *templateAccept,
			"status":            strings.TrimSpace(*status),
		}
		if *budgetHMC > 0 {
			payload["budget_hmc"] = *budgetHMC
		}
		if strings.TrimSpace(*targetID) != "" {
			payload["target_id"] = strings.TrimSpace(*targetID)
			payload["catalog"] = true
		} else if strings.TrimSpace(*sourceRel) != "" {
			payload["catalog"] = false
			payload["inventory_target"] = map[string]string{
				"path":   strings.TrimSpace(*sourceRel),
				"title":  filepathBase(*sourceRel),
				"source": "inventory",
			}
			if strings.TrimSpace(*repoPath) != "" || strings.TrimSpace(*gitURL) != "" {
				payload["repo"] = map[string]string{
					"path":    strings.TrimSpace(*repoPath),
					"git_url": strings.TrimSpace(*gitURL),
					"ref":     strings.TrimSpace(*ref),
				}
			} else if strings.TrimSpace(*repoPath) == "" && strings.TrimSpace(*gitURL) == "" {
				return fmt.Errorf("inventory hunt requires --repo-path or --git-url")
			}
		} else {
			return fmt.Errorf("hunt create requires --target (catalog) or --source (inventory)")
		}
		if strings.TrimSpace(*title) == "" {
			payload["title"] = "Hunt CLI " + strings.TrimSpace(*pkg)
		}
		b, code, err := apiDoAdmin(base, adm, http.MethodPost, "/api/hunt/campaigns", mustJSON(payload))
		if err != nil {
			return err
		}
		if code != http.StatusOK && code != http.StatusPaymentRequired {
			return fmt.Errorf("POST /api/hunt/campaigns HTTP %d: %s", code, strings.TrimSpace(string(b)))
		}
		fmt.Println(string(prettyJSON(b)))
		return nil
	default:
		return fmt.Errorf("unknown hunt subcommand %q", args[0])
	}
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, "json:", err)
		os.Exit(2)
	}
	return b
}

func filepathBase(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "inventory"
	}
	if i := strings.LastIndexAny(p, `/\`); i >= 0 && i+1 < len(p) {
		return p[i+1:]
	}
	return p
}
