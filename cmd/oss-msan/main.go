package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/fuzzupstream"
)

func main() {
	target := flag.String("target", "", "catalog target id (required)")
	repo := flag.String("repo", "", "repo root")
	out := flag.String("out", "", "output directory")
	wall := flag.Int("wall", 120, "wall seconds")
	maxSeeds := flag.Int("max-seeds", 256, "max seeds to replay")
	flag.Parse()

	repoRoot := strings.TrimSpace(*repo)
	if repoRoot == "" {
		repoRoot = os.Getenv("HACKME_REPO_ROOT")
	}
	if repoRoot == "" {
		repoRoot = "."
	}
	repoRoot, _ = filepath.Abs(repoRoot)
	tid := strings.TrimSpace(*target)
	if tid == "" {
		fmt.Fprintln(os.Stderr, "oss-msan: -target required")
		os.Exit(2)
	}

	m, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	t, err := m.TargetByID(tid)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*wall+120)*time.Second)
	defer cancel()
	rep, err := fuzzupstream.RunMSANCorpusSession(ctx, repoRoot, t, *out, *maxSeeds, *wall)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("verdict=%s hits=%d seeds=%d out_note=%s\n", rep.Verdict, len(rep.Hits), rep.SeedsTried, rep.Note)
	if rep.Verdict == "MSAN_TRIAGE" {
		os.Exit(0) // triage is success of the lane, not a CVE claim
	}
}
