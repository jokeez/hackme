package hunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMergeMinimizeCorpusPreservesHuntRunCrashes(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	crashDir := filepath.Join(corpus, ".hunt-run", "crashes")
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := os.WriteFile(filepath.Join(corpus, fmtSeedName(i)), []byte{byte(i), 0x80}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	crashPath := filepath.Join(crashDir, "crash-keep")
	if err := os.WriteFile(crashPath, []byte("keep-me-crash"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Fake fuzzer bin so MergeMinimizeCorpus proceeds past Stat; RunIsolated will
	// fail and restore raw — crashes must still survive the aside/restore path.
	bin := filepath.Join(dir, "fake-lf")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "0")
	_, _, _ = MergeMinimizeCorpus(context.Background(), bin, corpus)
	b, err := os.ReadFile(filepath.Join(corpus, ".hunt-run", "crashes", "crash-keep"))
	if err != nil {
		t.Fatalf("crash artifact lost after merge: %v", err)
	}
	if string(b) != "keep-me-crash" {
		t.Fatalf("crash bytes=%q", b)
	}
}

func TestMergeMinimizeCorpusSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	// CI hosts often deny bwrap RTM_NEWADDR; production hub keeps STRICT=1.
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "0")
	t.Setenv("HACKME_HUNT_BUILD_SANDBOX", "0")
	root := RepoRoot()
	bin := LibFuzzerImportBinPath(root, "mpack")
	if st, err := os.Stat(bin); err != nil || st.Size() == 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		var berr error
		bin, _, berr = BuildLibFuzzerImport(ctx, root, "mpack")
		if berr != nil {
			t.Skipf("build mpack lf: %v", berr)
		}
	}
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	// Near-duplicate seeds — merge should shrink.
	for i, b := range [][]byte{{0x80}, {0x80, 0x00}, {0x81}, {0x80}, []byte("{}"), []byte("{}"), []byte("[]")} {
		if err := os.WriteFile(filepath.Join(corpus, fmtSeedName(i)), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	before, after, err := MergeMinimizeCorpus(context.Background(), bin, corpus)
	if err != nil {
		t.Fatal(err)
	}
	if before < 5 {
		t.Fatalf("before=%d", before)
	}
	if after <= 0 || after > before {
		t.Fatalf("merge before=%d after=%d (expected shrink or equal unique set)", before, after)
	}
}
