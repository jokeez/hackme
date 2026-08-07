package store

import (
	"context"
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

func TestSetWALAutocheckpointAndPassive(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := SetWALAutocheckpoint(db, 500); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := CheckpointPassive(ctx, db); err != nil {
		t.Fatal(err)
	}
	if err := CheckpointTruncate(ctx, db); err != nil {
		t.Fatal(err)
	}
}
