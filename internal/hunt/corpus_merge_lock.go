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

const (
	rawMergePrefix = "corpus.raw-merge."
	huntRunPrefix  = "corpus.hunt-run."
)

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
// of the same input and are dropped. Also restores stale corpus.hunt-run.* asides
// into corpusDir/.hunt-run (failed restore rename left them forever — inode storm).
// Returns the number of dirs removed.
// AdoptStaleRawMergeDirs is exported for corpus-adopt-stale ops tooling.
func AdoptStaleRawMergeDirs(parent, corpusDir string, minAge time.Duration) int {
	ents, err := os.ReadDir(parent)
	if err != nil {
		return 0
	}
	removed := 0
	for _, e := range ents {
		name := e.Name()
		dir := filepath.Join(parent, name)
		info, err := os.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || time.Since(info.ModTime()) < minAge {
			continue
		}
		switch {
		case strings.HasPrefix(name, rawMergePrefix):
			moveCorpusFiles(parent, dir, corpusDir)
			if os.RemoveAll(dir) == nil {
				removed++
			}
		case strings.HasPrefix(name, huntRunPrefix):
			if adoptStaleHuntRunAside(parent, dir, corpusDir) {
				removed++
			}
		}
	}
	return removed
}

// adoptStaleHuntRunAside folds a leftover crash aside back into corpusDir/.hunt-run.
// Refuses symlink crash trees; never RemoveAll the aside on a failed restore rename.
func adoptStaleHuntRunAside(parent, aside, corpusDir string) bool {
	if parent != "" && !corpusPathConfined(parent, aside) {
		return false
	}
	for _, sub := range []string{"crashes", "hangs", "crash", "artifacts"} {
		p := filepath.Join(aside, sub)
		if st, err := os.Lstat(p); err == nil {
			if st.Mode()&os.ModeSymlink != 0 || !st.IsDir() {
				return false
			}
		}
	}
	dst := filepath.Join(corpusDir, ".hunt-run")
	if st, err := os.Lstat(dst); err == nil {
		if st.Mode()&os.ModeSymlink != 0 {
			return false
		}
		if st.IsDir() {
			// Merge crash files into the existing tree; then drop empty aside.
			for _, sub := range []string{"crashes", "hangs", "crash", "artifacts"} {
				srcSub := filepath.Join(aside, sub)
				if rejectSymlinkDir(srcSub) {
					continue
				}
				dstSub := filepath.Join(dst, sub)
				_ = os.MkdirAll(dstSub, 0o755)
				moveCorpusFiles(aside, srcSub, dstSub)
			}
			if err := os.RemoveAll(aside); err != nil {
				return false
			}
			return true
		}
	}
	if err := os.Rename(aside, dst); err != nil {
		return false
	}
	return true
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
