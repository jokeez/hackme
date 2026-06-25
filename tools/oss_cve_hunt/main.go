// Command oss_cve_hunt runs real upstream OSS CVE fuzzing (ASAN + mutation).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"hackme/internal/fuzzupstream"
)

func main() {
	repo := flag.String("repo", os.Getenv("HACKME_REPO_ROOT"), "repo root")
	out := flag.String("out", "", "output directory")
	targets := flag.String("targets", "all", "comma-separated target ids or all")
	budget := flag.Int("budget", 0, "iterations per target")
	timeLimit := flag.Int("time-limit", 0, "total time limit seconds")
	priority := flag.Int("priority-max", 0, "only targets with priority <= N (0=all)")
	flag.Parse()

	if *repo == "" {
		wd, _ := os.Getwd()
		*repo = wd
	}
	opts := fuzzupstream.HuntOptions{
		RepoRoot:         *repo,
		OutDir:           *out,
		TargetIDs:        fuzzupstream.TargetIDsFromFlags(*targets),
		BudgetIterations: *budget,
		TimeLimitSec:     *timeLimit,
		PriorityMax:      *priority,
	}
	rollup, err := fuzzupstream.RunHunt(context.Background(), opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "oss_cve_hunt: %v\n", err)
		os.Exit(2)
	}
	fmt.Printf("verdict=%s out=%s\n", rollup.Verdict, rollup.OutDir)
	fmt.Printf("summary: %s\n", rollup.Summary)
	if len(rollup.CVECandidates) > 0 {
		fmt.Printf("CVE candidates: %v\n", rollup.CVECandidates)
		for _, tr := range rollup.Targets {
			for _, c := range tr.Crashes {
				fmt.Printf("  [%s] %s input_len=%d iter=%d\n", c.TargetID, c.Sanitizer, c.InputLen, c.Iteration)
			}
		}
		os.Exit(1)
	}
	if len(rollup.BuildErrors) > 0 {
		for _, e := range rollup.BuildErrors {
			fmt.Fprintf(os.Stderr, "build error: %s\n", e)
		}
		os.Exit(3)
	}
}
