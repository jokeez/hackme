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

func TestReadCoordinatorWorkerTokenDoesNotLoadAdmin(t *testing.T) {
	dir := t.TempDir()
	secretDir := filepath.Join(dir, ".secrets")
	if err := os.MkdirAll(secretDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secretDir, coordinatorAdminTokenFile), []byte("admin-secret-only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := ReadCoordinatorWorkerToken(); got != "" {
		t.Fatalf("worker reader must not load admin file, got %q", got)
	}
	const want = "worker-token-value-xxxxxxxx"
	if err := os.WriteFile(filepath.Join(secretDir, coordinatorWorkerTokenFile), []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ReadCoordinatorWorkerToken(); got != want {
		t.Fatalf("ReadCoordinatorWorkerToken()=%q want %q", got, want)
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

func TestReadOrdersPublishToken(t *testing.T) {
	dir := t.TempDir()
	const want = "orders-publish-token-test-value!!"
	if err := os.WriteFile(filepath.Join(dir, "pool.orders.token"), []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := ReadOrdersPublishToken(); got != want {
		t.Fatalf("ReadOrdersPublishToken()=%q want %q", got, want)
	}
}

func TestReadOrdersPublishTokenRejectsWorldReadable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pool.orders.token"), []byte("leak\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := ReadOrdersPublishToken(); got != "" {
		t.Fatalf("world-readable pool.orders.token must be rejected, got %q", got)
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

func TestReadOrdersPrepaidHubSecretDistinctFromPublish(t *testing.T) {
	dir := t.TempDir()
	const pub = "orders-publish-fleet-token-xxxxx"
	const hub = "orders-prepaid-hub-secret-yyyyy"
	if err := os.WriteFile(filepath.Join(dir, "pool.orders.token"), []byte(pub+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ordersPrepaidHubSecretFile), []byte(hub+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := ReadOrdersPublishToken(); got != pub {
		t.Fatalf("publish=%q want %q", got, pub)
	}
	if got := ReadOrdersPrepaidHubSecret(); got != hub {
		t.Fatalf("prepaid hub=%q want %q", got, hub)
	}
	if ReadOrdersPrepaidHubSecret() == ReadOrdersPublishToken() {
		t.Fatal("prepaid hub secret must not alias publish token")
	}
}

func TestReadOrdersPrepaidHubSecretRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.WriteFile(target, []byte("symlink-prepaid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ordersPrepaidHubSecretFile)); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_REPO_ROOT", dir)
	if got := ReadOrdersPrepaidHubSecret(); got != "" {
		t.Fatalf("symlink prepaid secret must be rejected, got %q", got)
	}
}
