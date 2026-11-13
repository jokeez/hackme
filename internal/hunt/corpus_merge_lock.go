package hunt

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/workerlock"
)

// staleRawMergeAge: a corpus.raw-merge.* dir older than this cannot belong to a
// running merge (merge wall is 10 minutes), so its inputs are folded back.
const staleRawMergeAge = 30 * time.Minute

// StaleRawMergeAgeForOps is the default min-age for adopt-stale cleanup tools.
func StaleRawMergeAgeForOps() time.Duration { return staleRawMergeAge }

const rawMergePrefix = "corpus.raw-merge."

// acquireCorpusMergeLock takes a non-blocking, cross-process lock for merges of the
// corpus under parent. The lock file lives in <grandparent>/.corpus-merge-locks —
// outside parent, which is bind-mounted read-write into the untrusted fuzzer's cage
// during -merge=1, so a harness can never plant or swap it.
func acquireCorpusMergeLock(parent string) (*workerlock.Guard, error) {
	abs, err := filepath.Abs(parent)
	if err != nil {
		abs = parent
	}
	sum := sha256.Sum256([]byte(abs))
	id := filepath.Base(abs) + "-" + hex.EncodeToString(sum[:6])
	return workerlock.Acquire("corpus-merge", id, filepath.Join(filepath.Dir(abs), ".corpus-merge-locks"))
}

// adoptStaleRawMergeDirs folds the inputs of leftover corpus.raw-merge.* dirs (from
// crashed or raced merges) back into corpusDir and removes the dirs. Only regular
// files are moved (never symlinks); names that already exist are content-hash names
// of the same input and are dropped. Returns the number of dirs removed.
// AdoptStaleRawMergeDirs is exported for corpus-adopt-stale ops tooling.
func AdoptStaleRawMergeDirs(parent, corpusDir string, minAge time.Duration) int {
	ents, err := os.ReadDir(parent)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range ents {
		if !strings.HasPrefix(e.Name(), rawMergePrefix) {
			continue
		}
		dir := filepath.Join(parent, e.Name())
		info, err := os.Lstat(dir)
		if err != nil || !info.IsDir() || time.Since(info.ModTime()) < minAge {
			continue
		}
		moveCorpusFiles(parent, dir, corpusDir)
		if os.RemoveAll(dir) == nil {
			removed++
		}
	}
	return removed
}

// moveCorpusFiles moves regular files from src into dst (no overwrite).
func moveCorpusFiles(srcRoot, src, dst string) {
	if srcRoot != "" && !corpusPathConfined(srcRoot, src) {
		return
	}
	ents, err := os.ReadDir(src)
	if err != nil {
		return
	}
	for _, e := range ents {
		from := filepath.Join(src, e.Name())
		if _, ok := isRegularCorpusFileUnder(srcRoot, from); !ok {
			continue
		}
		to := filepath.Join(dst, e.Name())
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		_ = os.Rename(from, to)
	}
}
