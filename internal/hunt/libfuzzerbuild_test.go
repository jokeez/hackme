package hunt

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDedicatedLibFuzzerHarness(t *testing.T) {
	root := RepoRoot()
	if got := DedicatedLibFuzzerHarness(root, "cjson"); got == "" {
		t.Fatal("expected cjson dedicated harness")
	}
	if got := DedicatedLibFuzzerHarness(root, "nonexistent-target-xyz"); got != "" {
		t.Fatalf("unexpected harness: %s", got)
	}
}

func TestLibFuzzerSeedDirPaths(t *testing.T) {
	dir := t.TempDir()
	target := "jsmn"
	if got := LibFuzzerSeedDir(dir, target); got != filepath.Join(dir, ".cache", "hunt-lf-seeds", target) {
		t.Fatalf("seed dir=%q", got)
	}
	if got := LibFuzzerImportBinPath(dir, target); got != filepath.Join(dir, ".cache", "hunt-lf-import", target+"-libfuzzer-asan") {
		t.Fatalf("bin path=%q", got)
	}
}

func TestBuildSubprocessLibFuzzer(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not available")
	}
	root := RepoRoot()
	dir := t.TempDir()
	out := filepath.Join(dir, "subprocess-fuzzer")
	ctx := context.Background()
	if err := buildSubprocessLibFuzzer(ctx, root, out); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() == 0 {
		t.Fatalf("missing fuzzer binary: %v", err)
	}
}

func TestRunLibFuzzerImportSessionSynthetic(t *testing.T) {
	dir := t.TempDir()
	target := "gate-jsmn"
	corpusDir := LibFuzzerImportCorpusDir(dir, target)
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		t.Fatal(err)
	}
	seeds := [][]byte{[]byte(`{"a":1}`), []byte(`{"b":2}`), []byte("x")}
	for i, b := range seeds {
		if err := os.WriteFile(filepath.Join(corpusDir, fmtSeedName(i)), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := ExportLibFuzzerSeeds(dir, target, seeds)
	if err != nil || n != 3 {
		t.Fatalf("export=%d err=%v", n, err)
	}
	cfg := map[string]any{}
	merged, err := MergeLibFuzzerSeedCorpus(cfg, dir, target)
	if err != nil || merged != 3 {
		t.Fatalf("merge=%d err=%v cfg=%+v", merged, err, cfg)
	}
	if cfg["hunt_corpus_guided"] != true {
		t.Fatalf("expected guided defaults: %+v", cfg)
	}

	dir2 := t.TempDir()
	target2 := "session-import"
	corpus2 := LibFuzzerImportCorpusDir(dir2, target2)
	if err := os.MkdirAll(corpus2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corpus2, "a.bin"), []byte("one"), 0o644); err != nil {
		t.Fatal(err)
	}
	n2, err := ImportLibFuzzerCorpusFromSession(dir2, target2)
	if err != nil || n2 != 1 {
		t.Fatalf("session import=%d err=%v", n2, err)
	}
}

func fmtSeedName(i int) string {
	return fmt.Sprintf("seed-%02d.bin", i)
}
