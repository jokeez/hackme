package hunt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"
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
	// Unique raw dir so concurrent merges on the same parent cannot clobber each other.
	rawDir := filepath.Join(parent, fmt.Sprintf("corpus.raw-merge.%d.%d", os.Getpid(), time.Now().UnixNano()))
	// Preserve .hunt-run (crash artifacts) across rename→merge→RemoveAll(raw).
	// Without this, successful -merge=1 wiped LF crashes before engine upload.
	runAside := filepath.Join(parent, fmt.Sprintf("corpus.hunt-run.%d.%d", os.Getpid(), time.Now().UnixNano()))
	runSrc := filepath.Join(corpusDir, ".hunt-run")
	if st, serr := os.Lstat(runSrc); serr == nil && st.IsDir() {
		if rerr := os.Rename(runSrc, runAside); rerr != nil {
			runAside = ""
		}
	} else {
		runAside = ""
	}
	restoreHuntRun := func() {
		if runAside == "" {
			return
		}
		dst := filepath.Join(corpusDir, ".hunt-run")
		_ = os.RemoveAll(dst)
		if err := os.Rename(runAside, dst); err != nil {
			_ = os.RemoveAll(runAside)
		}
		runAside = ""
	}
	if err := os.Rename(corpusDir, rawDir); err != nil {
		restoreHuntRun()
		return before, before, err
	}
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		_ = os.Rename(rawDir, corpusDir) // best-effort restore
		restoreHuntRun()
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
			restoreHuntRun()
			return before, 0, fmt.Errorf("hunt: merge produced empty corpus: %s", out.String())
		}
		restoreHuntRun()
		after, _ = countCorpusFiles(corpusDir)
		return before, after, fmt.Errorf("hunt: merge failed (restored raw): %s", truncateMergeLog(out.String()))
	}
	_ = os.RemoveAll(rawDir)
	restoreHuntRun()
	_ = pruneCorpusDir(corpusDir, defaultMaxPersistentCorpus)
	after, _ = countCorpusFiles(corpusDir)
	_ = os.WriteFile(filepath.Join(parent, "merge_stats.json"),
		[]byte(fmt.Sprintf("{\n  \"before\": %d,\n  \"after\": %d,\n  \"max_cap\": %d\n}\n", before, after, defaultMaxPersistentCorpus)),
		0o644)
	return before, after, nil
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
	if maxKeep <= 0 {
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
		info, ok := isRegularCorpusFile(filepath.Join(dir, e.Name()))
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
