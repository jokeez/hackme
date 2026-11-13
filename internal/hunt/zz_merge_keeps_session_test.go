package hunt

import (
	"os"
	"path/filepath"
	"testing"
)

// Crash inputs live in corpus/.hunt-run/crashes. The merge renames the corpus away and deletes
// it afterwards; the session dirs must come back or every crash is lost before collection.
func TestKeepSessionDirsSurvivesMergeSwap(t *testing.T) {
	parent := t.TempDir()
	raw := filepath.Join(parent, "corpus.raw-merge.1.1")
	corpus := filepath.Join(parent, "corpus")
	crash := filepath.Join(raw, ".hunt-run", "crashes", "crash-abc")
	if err := os.MkdirAll(filepath.Dir(crash), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(crash, []byte("VR\x02N\x40AAAA"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(raw, "seed1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(raw, ".planted")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	keepSessionDirs(raw, corpus)
	if err := os.RemoveAll(raw); err != nil {
		t.Fatal(err)
	}
	if got := collectCrashArtifacts(corpus, filepath.Join(corpus, ".hunt-run"), 16); len(got) != 1 {
		t.Fatalf("crash artifact lost across merge swap: %v", got)
	}
	if _, err := os.Lstat(filepath.Join(corpus, ".planted")); err == nil {
		t.Fatal("symlinked dot-entry must not be carried into the corpus")
	}
	if _, err := os.Lstat(filepath.Join(corpus, "seed1")); err == nil {
		t.Fatal("plain corpus inputs come from the merge, not from keepSessionDirs")
	}
}
