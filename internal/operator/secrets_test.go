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

func TestReadCoordinatorAdminTokenRejectsWorldReadable(t *testing.T) {
	dir := t.TempDir()
	secretDir := filepath.Join(dir, ".secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(secretDir, coordinatorAdminTokenFile)
	if err := os.WriteFile(p, []byte("should-not-load\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := ReadCoordinatorAdminToken(); got != "" {
		t.Fatalf("expected reject of 0644 secret, got %q", got)
	}
}

func TestReadCoordinatorAdminTokenRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	secretDir := filepath.Join(dir, ".secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("symlink-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(secretDir, coordinatorAdminTokenFile)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := ReadCoordinatorAdminToken(); got != "" {
		t.Fatalf("expected reject of symlink secret, got %q", got)
	}
}

func TestOpenSecretFileRejectsGroupReadable(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "tok")
	if err := os.WriteFile(p, []byte("x\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := openSecretFile(p); err == nil {
		t.Fatal("expected error for 0640")
	}
}
