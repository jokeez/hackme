package fuzznative

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEvalReproAsanBinaryDupInputs(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	input := []byte{0x42, 0x42, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06}
	res, ok := evalReproAsanBinary("bitcoin", "bitcoin_tx_dup_inputs", input, nil, root)
	if !ok {
		t.Fatalf("asan binary unavailable: %s", res.Note)
	}
	if res.Status != StatusConfirmed {
		t.Fatalf("expected confirmed, got %s note=%s", res.Status, res.Note)
	}
}

func TestEvalReproAsanBinaryCleanInput(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	input := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	res, ok := evalReproAsanBinary("bitcoin", "bitcoin_tx_dup_inputs", input, nil, root)
	if !ok {
		t.Fatalf("asan binary unavailable: %s", res.Note)
	}
	if res.Status != StatusRejected {
		t.Fatalf("expected rejected, got %s", res.Status)
	}
}

func TestResolveHarnessPathBlocksEscape(t *testing.T) {
	root, err := repoRootForTest()
	if err != nil {
		t.Skip(err)
	}
	_, err = resolveHarnessPath(root, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected path block")
	}
}

func repoRootForTest() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", os.ErrNotExist
}
