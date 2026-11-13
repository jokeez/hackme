// Command hackme-fuzz-action is the engine behind the hackme-fuzz-action
// GitHub Action.
//
// MVP is dry-run only: resolve the Dig preset / guard pack / package, run the
// guard wasm through the same sandbox gate the pool uses, and print the
// security-audit payload that a real publish would send. No network, no
// tokens — safe to run on pull requests from forks.
//
// Exit codes: 0 plan ok · 1 wasm rejected by the gate · 2 bad inputs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"hackme/internal/fuzzingcli"
)

const runTimeout = 5 * time.Minute

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("hackme-fuzz-action", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var o Options
	var planOut string
	fs.StringVar(&o.Mode, "mode", modeDryRun, "only dry-run for now")
	fs.StringVar(&o.Workspace, "workspace", os.Getenv("GITHUB_WORKSPACE"), "root the -wasm path must stay inside (default $GITHUB_WORKSPACE, else cwd)")
	fs.StringVar(&o.HackmeRoot, "hackme-root", "", "hackme checkout with shipped guard packs (default: discover from cwd)")
	fs.StringVar(&o.WasmPath, "wasm", "", "custom guard wasm, relative to -workspace")
	fs.StringVar(&o.DigPreset, "dig-preset", "", "one-click Dig preset (scan_smoke, secrets, …)")
	fs.StringVar(&o.GuardPack, "guard-pack", "", "guard pack config (with -wasm) or pack to run")
	fs.StringVar(&o.Package, "package", "", "Dig package: scan|audit|deep (default from preset/pack, else scan)")
	fs.StringVar(&o.Title, "title", "", "campaign title")
	fs.StringVar(&planOut, "plan-out", "", "also write plan JSON to this file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}

	if o.Workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		o.Workspace = wd
	}
	if o.HackmeRoot == "" {
		o.HackmeRoot = fuzzingcli.DiscoverRepoRoot()
	}
	tmp, err := os.MkdirTemp("", "hackme-fuzz-action-")
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	defer os.RemoveAll(tmp)
	o.TmpDir = tmp

	ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
	defer cancel()
	plan, err := BuildPlan(ctx, o)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		if errors.Is(err, errUsage) {
			return 2
		}
		return 1
	}

	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	b = append(b, '\n')
	if _, err := stdout.Write(b); err != nil {
		return 1
	}
	if planOut != "" {
		if err := os.WriteFile(planOut, b, 0o644); err != nil {
			fmt.Fprintln(stderr, "error: write plan:", err)
			return 1
		}
	}
	if !plan.OK() {
		fmt.Fprintln(stderr, "wasm rejected by the pool gate:", plan.Wasm.Error)
		return 1
	}
	return 0
}
