package operator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadCoordinatorAdminToken(t *testing.T) {
	dir := t.TempDir()
	secretDir := filepath.Join(dir, ".secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	const want = "coord-token-test-value-32chars!!"
	if err := os.WriteFile(filepath.Join(secretDir, coordinatorAdminTokenFile), []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := ReadCoordinatorAdminToken(); got != want {
		t.Fatalf("ReadCoordinatorAdminToken()=%q want %q", got, want)
	}
}
