package hunt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreflightHarnessBinarySizeOK(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "aaaa.bin")
	payload := []byte("small-harness")
	if err := os.WriteFile(p, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	size, abs, err := PreflightHarnessBinarySize(dir, p)
	if err != nil {
		t.Fatal(err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("size=%d", size)
	}
	if abs == "" {
		t.Fatal("abs empty")
	}
}

func TestPreflightHarnessBinarySizeTooLarge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "huge.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	limit := int64(CoordinatorHarnessUploadMaxBytes) + 1
	if _, err := f.WriteAt([]byte{1}, limit-1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()
	size, _, err := PreflightHarnessBinarySize(dir, p)
	if err == nil {
		t.Fatal("expected too-large error")
	}
	if size != limit {
		t.Fatalf("size=%d want %d", size, limit)
	}
	if !strings.Contains(err.Error(), "harness_too_large") {
		t.Fatalf("err=%v", err)
	}
}

func TestPreflightHarnessBinarySizeRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "cache")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.bin")
	if err := os.WriteFile(secret, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := PreflightHarnessBinarySize(dir, filepath.Join(dir, "..", filepath.Base(outside), "secret.bin")); err == nil {
		// Rel path that escapes via .. must fail MustUnderRoot.
		t.Fatal("expected path-outside-root error")
	}
	if _, _, err := PreflightHarnessBinarySize(dir, secret); err == nil {
		t.Fatal("expected absolute outside-root reject")
	}
	_ = nested
}

func TestCoordinatorHarnessUploadMaxBytesAligned(t *testing.T) {
	if CoordinatorHarnessUploadMaxBytes != 36<<20 {
		t.Fatalf("max=%d want %d", CoordinatorHarnessUploadMaxBytes, 36<<20)
	}
	if maxHarnessArtifactBytes != CoordinatorHarnessUploadMaxBytes {
		t.Fatalf("artifact max %d != coordinator %d", maxHarnessArtifactBytes, CoordinatorHarnessUploadMaxBytes)
	}
}
