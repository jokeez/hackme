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
	if err := PutHarnessArtifact(ctx, db, "abc123", data, "fuzz/target.c"); err != nil {
		t.Fatal(err)
	}
	got, err := GetHarnessArtifact(ctx, db, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(data) {
		t.Fatalf("blob mismatch")
	}
}

func TestHarnessFetchURL(t *testing.T) {
	u := HarnessFetchURL("deadbeef")
	if u != "/api/fuzz/pool/hunt/harness/deadbeef" {
		t.Fatalf("url=%q", u)
	}
}
