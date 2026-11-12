package hunt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/workerlock"
)

func TestAdoptStaleRawMergeDirsFoldsInputsBack(t *testing.T) {
	parent := t.TempDir()
	corpus := filepath.Join(parent, "corpus")
	must(t, os.MkdirAll(corpus, 0o755))
	must(t, os.WriteFile(filepath.Join(corpus, "a"), []byte("a"), 0o600))

	old := filepath.Join(parent, rawMergePrefix+"1.1")
	must(t, os.MkdirAll(old, 0o755))
	must(t, os.WriteFile(filepath.Join(old, "a"), []byte("a"), 0o600)) // duplicate name
	must(t, os.WriteFile(filepath.Join(old, "b"), []byte("b"), 0o600))
	secret := filepath.Join(t.TempDir(), "secret")
	must(t, os.WriteFile(secret, []byte("s"), 0o600))
	must(t, os.Symlink(secret, filepath.Join(old, "evil")))
	past := time.Now().Add(-2 * time.Hour)
	must(t, os.Chtimes(old, past, past))

	fresh := filepath.Join(parent, rawMergePrefix+"2.2") // may belong to a running merge
	must(t, os.MkdirAll(fresh, 0o755))
	must(t, os.WriteFile(filepath.Join(fresh, "c"), []byte("c"), 0o600))

	if n := AdoptStaleRawMergeDirs(parent, corpus, staleRawMergeAge); n != 1 {
		t.Fatalf("removed %d dirs, want 1", n)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("stale raw dir still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "c")); err != nil {
		t.Fatalf("fresh raw dir must be left alone: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(corpus, "b")); err != nil || string(b) != "b" {
		t.Fatalf("input b not folded back: %q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(corpus, "evil")); !os.IsNotExist(err) {
		t.Fatalf("symlink must never be adopted into the corpus")
	}
	if b, _ := os.ReadFile(secret); string(b) != "s" {
		t.Fatalf("symlink target touched")
	}
}

func TestCorpusMergeLockIsExclusiveAndOutsideCage(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "zlib")
	must(t, os.MkdirAll(parent, 0o755))
	g, err := acquireCorpusMergeLock(parent)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(filepath.Dir(g.Path())) == parent || filepath.Dir(g.Path()) == parent {
		t.Fatalf("lock file %s must live outside the bind-mounted parent", g.Path())
	}
	if _, err := acquireCorpusMergeLock(parent); !errors.Is(err, workerlock.ErrAlreadyRunning) {
		t.Fatalf("second acquire: %v, want ErrAlreadyRunning", err)
	}
	g.Release()
	g2, err := acquireCorpusMergeLock(parent)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	g2.Release()
}

func TestMergeSkipsWhileAnotherWorkerHoldsTheLock(t *testing.T) {
	parent := t.TempDir()
	corpus := filepath.Join(parent, "corpus")
	must(t, os.MkdirAll(corpus, 0o755))
	for _, n := range []string{"a", "b", "c"} {
		must(t, os.WriteFile(filepath.Join(corpus, n), []byte(n), 0o600))
	}
	bin := filepath.Join(t.TempDir(), "fuzzer")
	must(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755))
	g, err := acquireCorpusMergeLock(parent)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Release()
	before, after, err := MergeMinimizeCorpus(context.Background(), bin, corpus)
	if err != nil || before != 3 || after != 3 {
		t.Fatalf("busy lock: before=%d after=%d err=%v, want 3/3/nil", before, after, err)
	}
	ents, _ := os.ReadDir(parent)
	for _, e := range ents {
		if e.Name() != "corpus" {
			t.Fatalf("unexpected entry %s — merge must not touch the corpus while locked", e.Name())
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// Regression for the bootstrap inode exhaustion: four workers merging one shared
// corpus at once must never strand corpus.raw-merge.* dirs or lose the corpus.
func TestConcurrentMergesLeaveNoRawDirs(t *testing.T) {
	bin := LibFuzzerImportBinPath(RepoRoot(), "mpack")
	if st, err := os.Stat(bin); err != nil || st.Size() == 0 {
		t.Skip("mpack libFuzzer binary not built (see TestMergeMinimizeCorpusSmoke)")
	}
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "0")
	t.Setenv("HACKME_HUNT_BUILD_SANDBOX", "0")
	parent := t.TempDir()
	corpus := filepath.Join(parent, "corpus")
	for round := 0; round < 5; round++ {
		must(t, os.MkdirAll(corpus, 0o755))
		for i := 0; i < 40; i++ {
			name := fmtSeedName(round*100 + i)
			must(t, os.WriteFile(filepath.Join(corpus, name), []byte{byte(0x80 + i%8), byte(i)}, 0o600))
		}
		done := make(chan struct{})
		for w := 0; w < 4; w++ {
			go func() {
				defer func() { done <- struct{}{} }()
				_, _, _ = MergeMinimizeCorpus(context.Background(), bin, corpus)
			}()
		}
		for w := 0; w < 4; w++ {
			<-done
		}
		ents, _ := os.ReadDir(parent)
		for _, e := range ents {
			if len(e.Name()) > len(rawMergePrefix) && e.Name()[:len(rawMergePrefix)] == rawMergePrefix {
				t.Fatalf("round %d: stranded %s", round, e.Name())
			}
		}
		if n, _ := countCorpusFiles(corpus); n == 0 {
			t.Fatalf("round %d: corpus lost", round)
		}
	}
}
