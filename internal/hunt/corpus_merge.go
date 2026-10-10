package hunt

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	if err := os.Rename(corpusDir, rawDir); err != nil {
		return before, before, err
	}
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		_ = os.Rename(rawDir, corpusDir) // best-effort restore
		return before, before, err
	}

	runCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(runCtx, fuzzerBin, corpusDir, rawDir, "-merge=1")
	cmd.Env = append(os.Environ(),
		"ASAN_OPTIONS=detect_leaks=0:halt_on_error=0:allocator_may_return_null=1",
		"UBSAN_OPTIONS=halt_on_error=0",
	)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	_ = cmd.Run() // merge may exit non-zero; keep whatever landed in corpusDir

	after, _ = countCorpusFiles(corpusDir)
	if after == 0 {
		// Merge failed — restore raw.
		_ = os.RemoveAll(corpusDir)
		if rerr := os.Rename(rawDir, corpusDir); rerr != nil {
			return before, 0, fmt.Errorf("hunt: merge produced empty corpus: %s", out.String())
		}
		after, _ = countCorpusFiles(corpusDir)
		return before, after, fmt.Errorf("hunt: merge failed (restored raw): %s", truncateMergeLog(out.String()))
	}
	_ = os.RemoveAll(rawDir)
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
		info, err := e.Info()
		if err != nil {
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
