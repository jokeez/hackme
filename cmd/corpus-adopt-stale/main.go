// corpus-adopt-stale folds stale corpus.raw-merge.* dirs back into persistent
// libFuzzer corpora (same logic as hunt.MergeMinimizeCorpus startup). Safe ops
// for bootstrap/hub inode recovery — never rm -rf merge temps blindly.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hackme/internal/hunt"
)

func main() {
	root := flag.String("repo", "", "repo root (default HACKME_REPO_ROOT or cwd)")
	minAge := flag.Duration("min-age", hunt.StaleRawMergeAgeForOps(), "only adopt raw-merge dirs older than this")
	dry := flag.Bool("dry-run", false, "count dirs only, do not fold or remove")
	flag.Parse()
	repo := strings.TrimSpace(*root)
	if repo == "" {
		repo = strings.TrimSpace(os.Getenv("HACKME_REPO_ROOT"))
	}
	if repo == "" {
		wd, _ := os.Getwd()
		repo = wd
	}
	lf := filepath.Join(repo, "reports", "oss-cve-libfuzzer")
	ents, err := os.ReadDir(lf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read %s: %v\n", lf, err)
		os.Exit(1)
	}
	var parents, adopted int
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		parent := filepath.Join(lf, e.Name())
		corpus := filepath.Join(parent, "corpus")
		if st, err := os.Stat(corpus); err != nil || !st.IsDir() {
			continue
		}
		n := countRawMerge(parent)
		if n == 0 {
			continue
		}
		parents++
		if *dry {
			fmt.Printf("%s raw_merge_dirs=%d\n", e.Name(), n)
			continue
		}
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "%s: mkdir corpus: %v\n", e.Name(), err)
			continue
		}
		removed := hunt.AdoptStaleRawMergeDirs(parent, corpus, *minAge)
		adopted += removed
		fmt.Printf("%s adopted_dirs=%d remaining_raw=%d\n", e.Name(), removed, countRawMerge(parent))
	}
	if *dry {
		fmt.Printf("dry-run targets=%d\n", parents)
		return
	}
	fmt.Printf("done targets=%d adopted_dirs=%d\n", parents, adopted)
}

func countRawMerge(parent string) int {
	ents, err := os.ReadDir(parent)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), "corpus.raw-merge.") {
			n++
		}
	}
	return n
}
