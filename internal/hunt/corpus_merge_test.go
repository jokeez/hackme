package hunt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestMergeMinimizeCorpusSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
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
