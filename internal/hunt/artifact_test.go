package hunt

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestHarnessArtifactRoundTrip(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hunt-artifact.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	data := []byte{0x7f, 'E', 'L', 'F'}
	if err := PutHarnessArtifact(ctx, db, "abc12345", data, "fuzz/target.c"); err != nil {
		t.Fatal(err)
	}
	got, err := GetHarnessArtifact(ctx, db, "abc12345")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("blob mismatch")
	}
	if err := PutHarnessArtifact(ctx, db, "../etc/passwd", data, "x"); err == nil {
		t.Fatal("path traversal hash must be rejected")
	}
	if ValidHarnessHash("ab/cd") || ValidHarnessHash("short") {
		t.Fatal("invalid hashes accepted")
	}
}

func TestSafeHarnessFetchURL(t *testing.T) {
	if !SafeHarnessFetchURL("/api/fuzz/pool/hunt/harness/abc12345") {
		t.Fatal("relative harness path")
	}
	if SafeHarnessFetchURL("http://127.0.0.1/api/fuzz/pool/hunt/harness/abc12345") {
		t.Fatal("loopback must be rejected without matching coordinator env")
	}
	if SafeHarnessFetchURL("https://evil.example/ssrf") {
		t.Fatal("non-harness path")
	}
	if SafeHarnessFetchURL("http://169.254.169.254/api/fuzz/pool/hunt/harness/abc12345") {
		t.Fatal("link-local SSRF")
	}
}

func TestPutHarnessArtifactRejectsOverwrite(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hunt-ow.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := PutHarnessArtifact(ctx, db, "abc12345", []byte("AAAA"), "a.c"); err != nil {
		t.Fatal(err)
	}
	if err := PutHarnessArtifact(ctx, db, "abc12345", []byte("BBBB"), "a.c"); err == nil {
		t.Fatal("different blob overwrite must fail")
	}
	if err := PutHarnessArtifact(ctx, db, "abc12345", []byte("AAAA"), "a.c"); err != nil {
		t.Fatal("identical re-publish must be ok")
	}
}

func TestHarnessFetchURL(t *testing.T) {
	u := HarnessFetchURL("deadbeef")
	if u != "/api/fuzz/pool/hunt/harness/deadbeef" {
		t.Fatalf("url=%q", u)
	}
}
