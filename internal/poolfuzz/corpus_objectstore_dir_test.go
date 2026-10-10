package poolfuzz

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultCorpusObjectDirBesideDB_scopedPerDB(t *testing.T) {
	root := t.TempDir()
	db1 := filepath.Join(root, "a.db")
	db2 := filepath.Join(root, "b.db")
	if err := os.WriteFile(db1, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db2, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d1 := DefaultCorpusObjectDirBesideDB(db1)
	d2 := DefaultCorpusObjectDirBesideDB(db2)
	if d1 == d2 {
		t.Fatalf("expected distinct corpus dirs, got %q for both", d1)
	}
	if d1 != db1+".corpus-objects" {
		t.Fatalf("got %q want %q", d1, db1+".corpus-objects")
	}
}

func TestDefaultCorpusObjectDirBesideDB_ignoresTempLegacy(t *testing.T) {
	root := t.TempDir()
	db := filepath.Join(root, "fuzz.db")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "corpus-objects")
	if err := os.MkdirAll(filepath.Join(legacy, "snap", "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "snap", "ab", "marker"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := DefaultCorpusObjectDirBesideDB(db)
	want := db + ".corpus-objects"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if st, err := os.Stat(legacy); err != nil || !st.IsDir() {
		t.Fatalf("temp legacy corpus-objects must not be renamed/migrated")
	}
}
