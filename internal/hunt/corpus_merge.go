package hunt

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"hackme/internal/workerlock"
)

const (
	// defaultMaxPersistentCorpus caps on-disk LF research corpora after -merge=1.
	defaultMaxPersistentCorpus = 4096
)

// MergeMinimizeCorpus runs libFuzzer -merge=1 to dedupe/minimize a corpus directory
// by coverage-preserving unique inputs. Returns (before, after) file counts.
func MergeMinimizeCorpus(ctx context.Context, fuzzerBin, corpusDir string) (before, after int, err error) {
	before, _ = countCorpusFiles(corpusDir)
	if before <= 1 {
		return before, before, nil
	}
	if st, err := os.Stat(fuzzerBin); err != nil || !st.Mode().IsRegular() {
		return before, before, fmt.Errorf("hunt: merge: fuzzer bin missing: %s", fuzzerBin)
	}
	parent := filepath.Dir(corpusDir)
	// Several workers (workerfuzz -worker N) share one persistent corpus. Without a
	// cross-process lock their rename/restore steps interleave: restores fail on a
	// non-empty corpus and every failed merge strands a corpus.raw-merge.* dir — on
	// the bootstrap host that leaked ~2M files and exhausted the filesystem's inodes.
	// Only one merge per corpus at a time; a busy lock means another worker is
	// already minimizing this corpus, so skip.
	guard, lerr := acquireCorpusMergeLock(parent)
	if lerr != nil {
		if errors.Is(lerr, workerlock.ErrAlreadyRunning) {
			return before, before, nil
		}
		return before, before, fmt.Errorf("hunt: merge lock: %w", lerr)
	}
	defer guard.Release()
	if n := AdoptStaleRawMergeDirs(parent, corpusDir, staleRawMergeAge); n > 0 {
		before, _ = countCorpusFiles(corpusDir)
	}
	// Unique raw dir so concurrent merges on the same parent cannot clobber each other.
	rawDir := filepath.Join(parent, fmt.Sprintf("corpus.raw-merge.%d.%d", os.Getpid(), time.Now().UnixNano()))
	if err := os.Rename(corpusDir, rawDir); err != nil {
		return before, before, err
	}
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		_ = os.Rename(rawDir, corpusDir) // best-effort restore
		return before, before, err
	}

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	var out bytes.Buffer
	env := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/tmp",
		"ASAN_OPTIONS=detect_leaks=0:halt_on_error=0:allocator_may_return_null=1",
		"UBSAN_OPTIONS=halt_on_error=0",
	}
	// Same cage as LF session — never bare-exec the untrusted fuzzer for -merge=1.
	_ = RunIsolated(runCtx, IsolateOpts{
		Bin:         fuzzerBin,
		Args:        []string{corpusDir, rawDir, "-merge=1"},
		Env:         env,
		Wall:        10 * time.Minute,
		Stdout:      &out,
		Stderr:      &out,
		AllowBare:   false,
		ExtraBind:   []string{corpusDir, rawDir, parent},
		ExtraRoBind: []string{filepath.Dir(fuzzerBin)},
	})

	after, _ = countCorpusFiles(corpusDir)
	if after == 0 {
		// Merge failed — restore raw.
		_ = os.RemoveAll(corpusDir)
		if rerr := os.Rename(rawDir, corpusDir); rerr != nil {
			// Never strand the raw inputs: fold them back file by file.
			_ = os.MkdirAll(corpusDir, 0o755)
			moveCorpusFiles(parent, rawDir, corpusDir)
			if derr := os.RemoveAll(rawDir); derr != nil {
				return before, 0, fmt.Errorf("hunt: merge restore: %v (raw dir left: %v)", rerr, derr)
			}
		}
		after, _ = countCorpusFiles(corpusDir)
		return before, after, fmt.Errorf("hunt: merge failed (restored raw): %s", truncateMergeLog(out.String()))
	}
	// The session's own artifacts live in dot-dirs inside the corpus (.hunt-run/crashes with
	// the crash inputs libFuzzer just wrote). They were renamed away with the raw corpus; move
	// them back before rawDir is deleted. Without this every session whose corpus had more
	// than 8 files lost its crashes before RunEngineShard collected them — the crash shard then
	// looked like a short wall ("lf_wall_short") and was skipped, on miners and local Hunt
	// alike (customer e2e 2026-11-13: a planted heap overflow found in <1 s, never reported).
	keepSessionDirs(rawDir, corpusDir)
	var cleanupErr error
	if derr := os.RemoveAll(rawDir); derr != nil {
		cleanupErr = fmt.Errorf("hunt: merge: remove %s: %w", filepath.Base(rawDir), derr)
	}
	_ = pruneCorpusDir(corpusDir, defaultMaxPersistentCorpus)
	after, _ = countCorpusFiles(corpusDir)
	_ = writeFileBeneath(parent, filepath.Join(parent, "merge_stats.json"),
		[]byte(fmt.Sprintf("{\n  \"before\": %d,\n  \"after\": %d,\n  \"max_cap\": %d\n}\n", before, after, defaultMaxPersistentCorpus)),
		0o644)
	return before, after, cleanupErr
}

// MergeMinimizePersistentCorpus builds (or reuses) the LF binary and merges the durable corpus.
func MergeMinimizePersistentCorpus(ctx context.Context, repoRoot, targetID string) (before, after int, err error) {
	bin, _, err := BuildLibFuzzerImport(ctx, repoRoot, targetID)
	if err != nil {
		return 0, 0, err
	}
	dir := PersistentLibFuzzerCorpusDir(repoRoot, targetID)
	return MergeMinimizeCorpus(ctx, bin, dir)
}

func pruneCorpusDir(dir string, maxKeep int) error {
	return pruneCorpusDirUnder(dir, dir, maxKeep)
}

func pruneCorpusDirUnder(root, dir string, maxKeep int) error {
	if maxKeep <= 0 {
		return nil
	}
	if root != "" && !corpusPathConfined(root, dir) {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type item struct {
		name string
		mod  int64
		size int64
	}
	var files []item
	for _, e := range ents {
		if e.IsDir() || len(e.Name()) == 0 || e.Name()[0] == '.' {
			continue
		}
		info, ok := isRegularCorpusFileUnder(root, filepath.Join(dir, e.Name()))
		if !ok {
			continue
		}
		files = append(files, item{name: e.Name(), mod: info.ModTime().UnixNano(), size: info.Size()})
	}
	if len(files) <= maxKeep {
		return nil
	}
	// Keep newest first.
	for i := 0; i < len(files); i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].mod > files[i].mod {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
	for _, f := range files[maxKeep:] {
		_ = os.Remove(filepath.Join(dir, f.name))
	}
	return nil
}

func truncateMergeLog(s string) string {
	if len(s) > 400 {
		return s[len(s)-400:]
	}
	return s
}

// keepSessionDirs moves dot-directories (session artifacts, not corpus inputs) from the raw
// pre-merge dir back into the merged corpus dir. Plain renames within one parent; entries
// that are not real directories (e.g. a harness-planted symlink) are left to be deleted.
func keepSessionDirs(rawDir, corpusDir string) {
	ents, err := os.ReadDir(rawDir)
	if err != nil {
		return
	}
	for _, e := range ents {
		name := e.Name()
		if !strings.HasPrefix(name, ".") || name == "." || name == ".." {
			continue
		}
		src := filepath.Join(rawDir, name)
		if st, err := os.Lstat(src); err != nil || !st.IsDir() {
			continue
		}
		dst := filepath.Join(corpusDir, name)
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		_ = os.Rename(src, dst)
	}
}
