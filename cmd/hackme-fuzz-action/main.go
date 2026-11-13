// Command hackme-fuzz-action is the engine behind the hackme-fuzz-action
// GitHub Action.
//
// MVP is dry-run only: resolve the Dig preset / guard pack / package, run the
// guard wasm through the same sandbox gate the pool uses, and print the
// security-audit payload that a real publish would send. No network, no
// tokens — safe to run on pull requests from forks.
//
// -mode gate waits for an already published campaign and turns its CI gate
// into the verdict (report token from $HACKME_REPORT_TOKEN).
//
// Exit codes: 0 ok/pass · 1 wasm rejected / gate failed / not finished · 2 bad inputs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
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
	var planOut, markdownOut, githubOutput string
	fs.StringVar(&o.Mode, "mode", modeDryRun, "dry-run | gate")
	fs.StringVar(&o.Workspace, "workspace", os.Getenv("GITHUB_WORKSPACE"), "root the -wasm path must stay inside (default $GITHUB_WORKSPACE, else cwd)")
	fs.StringVar(&o.HackmeRoot, "hackme-root", "", "hackme checkout with shipped guard packs (default: discover from cwd)")
	fs.StringVar(&o.WasmPath, "wasm", "", "custom guard wasm, relative to -workspace")
	fs.StringVar(&o.BuildSrc, "source", "", "single-file guard source to compile, relative to -workspace")
	fs.StringVar(&o.BuildLang, "language", "", "language of -source: rust|c|cpp")
	fs.StringVar(&o.DigPreset, "dig-preset", "", "one-click Dig preset (scan_smoke, secrets, …)")
	fs.StringVar(&o.GuardPack, "guard-pack", "", "guard pack config (with -wasm) or pack to run")
	fs.StringVar(&o.Package, "package", "", "Dig package: scan|audit|deep (default from preset/pack, else scan)")
	fs.StringVar(&o.Title, "title", "", "campaign title")
	fs.StringVar(&planOut, "plan-out", "", "also write plan JSON to this file")
	fs.StringVar(&markdownOut, "markdown-out", "", "write the PR comment / job summary markdown to this file")
	fs.StringVar(&githubOutput, "github-output", "", "append step outputs here (pass $GITHUB_OUTPUT)")
	var g GateOptions
	fs.StringVar(&g.BaseURL, "hackme-url", "", "gate mode: Orders/node base URL (https)")
	fs.StringVar(&g.CampaignID, "campaign-id", "", "gate mode: campaign to wait for")
	fs.DurationVar(&g.Timeout, "gate-timeout", 30*time.Minute, "gate mode: max wait for the campaign to finish")
	fs.DurationVar(&g.Poll, "gate-poll", 20*time.Second, "gate mode: pulse poll interval")
	fs.IntVar(&g.MaxCritical, "max-critical", 0, "gate mode: allowed crash-class critical findings")
	fs.IntVar(&g.MaxHigh, "max-high", 0, "gate mode: allowed crash-class high findings")
	fs.IntVar(&g.MinRunsDone, "min-runs-done", -1, "gate mode: required runs (-1 = full budget)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %v\n", fs.Args())
		return 2
	}
	if strings.EqualFold(strings.TrimSpace(o.Mode), modeGate) {
		g.Token = os.Getenv(reportTokenEnv)
		return runGate(g, markdownOut, githubOutput, stdout, stderr)
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
	if markdownOut != "" {
		if err := os.WriteFile(markdownOut, []byte(RenderMarkdown(plan)), 0o644); err != nil {
			fmt.Fprintln(stderr, "error: write markdown:", err)
			return 1
		}
	}
	if githubOutput != "" {
		if err := appendFile(githubOutput, func(w io.Writer) error { return writeOutputs(w, Outputs(plan)) }); err != nil {
			fmt.Fprintln(stderr, "error: write outputs:", err)
			return 1
		}
	}
	if !plan.OK() {
		fmt.Fprintln(stderr, "wasm rejected by the pool gate:", plan.Wasm.Error)
		return 1
	}
	return 0
}

// maxGateWait stays under GitHub's 6h job limit.
const maxGateWait = 6 * time.Hour

func runGate(g GateOptions, markdownOut, githubOutput string, stdout, stderr io.Writer) int {
	if g.Timeout <= 0 || g.Timeout > maxGateWait || g.Poll <= 0 || g.Poll > g.Timeout {
		fmt.Fprintf(stderr, "error: gate-timeout must be in (0, %s] and gate-poll in (0, gate-timeout]\n", maxGateWait)
		return 2
	}
	if g.MaxCritical < 0 || g.MaxHigh < 0 || g.MinRunsDone < -1 {
		fmt.Fprintln(stderr, "error: max-critical/max-high must be >= 0, min-runs-done >= -1")
		return 2
	}
	c, err := newGateClient(g)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	res, err := WaitGate(context.Background(), c, g)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	b, _ := json.MarshalIndent(res, "", "  ")
	_, _ = stdout.Write(append(b, '\n'))
	if markdownOut != "" {
		if err := os.WriteFile(markdownOut, []byte(RenderGateMarkdown(res)), 0o644); err != nil {
			fmt.Fprintln(stderr, "error: write markdown:", err)
			return 1
		}
	}
	if githubOutput != "" {
		if err := appendFile(githubOutput, func(w io.Writer) error { return writeOutputs(w, GateOutputs(res)) }); err != nil {
			fmt.Fprintln(stderr, "error: write outputs:", err)
			return 1
		}
	}
	if !res.OK() {
		fmt.Fprintln(stderr, "gate did not pass:", strings.Join(res.Reasons, "; "))
		return 1
	}
	return 0
}
