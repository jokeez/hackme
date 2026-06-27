package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWALSizeBytes_missing(t *testing.T) {
	dir := t.TempDir()
	if got := WALSizeBytes(filepath.Join(dir, "nope.db")); got != 0 {
		t.Fatalf("want 0 got %d", got)
	}
}

func TestWALSizeBytes_present(t *testing.T) {
	dir := t.TempDir()
	db := filepath.Join(dir, "x.db")
	wal := WALPath(db)
	if err := os.WriteFile(wal, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := WALSizeBytes(db); got != 5 {
		t.Fatalf("want 5 got %d", got)
	}
}
