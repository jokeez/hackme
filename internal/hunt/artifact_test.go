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

func TestHarnessFetchURL(t *testing.T) {
	u := HarnessFetchURL("deadbeef")
	if u != "/api/fuzz/pool/hunt/harness/deadbeef" {
		t.Fatalf("url=%q", u)
	}
}
