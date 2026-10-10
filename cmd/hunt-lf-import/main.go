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
	persist := flag.Bool("persist", false, "use durable reports/oss-cve-libfuzzer/<target>/corpus (no wipe)")
	buildOnly := flag.Bool("build-only", false, "compile/reuse libFuzzer binary and exit")
	mergeOnly := flag.Bool("merge-only", false, "libFuzzer -merge=1 dedupe/minimize persistent corpus, then import")
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

	if *buildOnly {
		bin, _, err := hunt.BuildLibFuzzerImport(ctx, repoRoot, targetID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(bin)
		return
	}

	if *mergeOnly {
		before, after, err := hunt.MergeMinimizePersistentCorpus(ctx, repoRoot, targetID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		// Point scratch import dir at persistent corpus for ImportLibFuzzerCorpusFromSession.
		persistDir := hunt.PersistentLibFuzzerCorpusDir(repoRoot, targetID)
		scratch := hunt.LibFuzzerImportCorpusDir(repoRoot, targetID)
		_ = os.RemoveAll(scratch)
		_ = os.MkdirAll(filepath.Dir(scratch), 0o755)
		_ = os.Symlink(persistDir, scratch)
		n, ierr := hunt.ImportLibFuzzerCorpusFromSession(repoRoot, targetID)
		if ierr != nil {
			fmt.Fprintf(os.Stderr, "merge ok before=%d after=%d; import: %v\n", before, after, ierr)
			fmt.Println(after)
			return
		}
		fmt.Fprintf(os.Stderr, "merge before=%d after=%d imported=%d\n", before, after, n)
		fmt.Println(n)
		return
	}

	if *importOnly {
		n, err := hunt.ImportLibFuzzerCorpusFromSession(repoRoot, targetID)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(n)
		return
	}

	var (
		n   int
		err error
	)
	if *persist {
		n, err = hunt.RunPersistentLibFuzzerSession(ctx, repoRoot, targetID, *wall)
	} else {
		n, err = hunt.RunLibFuzzerImportSession(ctx, repoRoot, targetID, *wall)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(n)
}
