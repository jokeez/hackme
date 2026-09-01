package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/hunt"
)

func main() {
	target := flag.String("target", "", "catalog target id (required)")
	wall := flag.Int("wall", 120, "libFuzzer wall seconds")
	importOnly := flag.Bool("import-only", false, "import existing session corpus without running libFuzzer")
	repo := flag.String("repo", "", "repo root (default: HACKME_REPO_ROOT or cwd)")
	flag.Parse()

	repoRoot := strings.TrimSpace(*repo)
	if repoRoot == "" {
		repoRoot = os.Getenv("HACKME_REPO_ROOT")
	}
	if repoRoot == "" {
		repoRoot = "."
	}
	repoRoot, _ = filepath.Abs(repoRoot)
	targetID := strings.TrimSpace(*target)
	if targetID == "" {
		fmt.Fprintln(os.Stderr, "hunt-lf-import: -target required")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*wall+180)*time.Second)
	defer cancel()

	if *importOnly {
		n, err := hunt.ImportLibFuzzerCorpusFromSession(repoRoot, targetID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(n)
		return
	}

	n, err := hunt.RunLibFuzzerImportSession(ctx, repoRoot, targetID, *wall)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(n)
}
