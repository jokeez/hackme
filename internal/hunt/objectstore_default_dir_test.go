package hunt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultHarnessObjectDirBesideDB_scopedPerDB(t *testing.T) {
	root := t.TempDir()
	db1 := filepath.Join(root, "a.db")
	db2 := filepath.Join(root, "b.db")
	if err := os.WriteFile(db1, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(db2, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	d1 := DefaultHarnessObjectDirBesideDB(db1)
	d2 := DefaultHarnessObjectDirBesideDB(db2)
	if d1 == d2 {
		t.Fatalf("expected distinct harness dirs, got %q for both", d1)
	}
	if d1 != db1+".harness" {
		t.Fatalf("got %q want %q", d1, db1+".harness")
	}
}

func TestDefaultHarnessObjectDirBesideDB_ignoresTempLegacy(t *testing.T) {
	// t.TempDir is under the process temp root — must not adopt a shared harness/.
	root := t.TempDir()
	db := filepath.Join(root, "fuzz.db")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "harness")
	if err := os.MkdirAll(filepath.Join(legacy, "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "ab", "marker"), []byte("stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := DefaultHarnessObjectDirBesideDB(db)
	want := db + ".harness"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if !dirExists(legacy) {
		t.Fatalf("temp legacy harness must not be renamed/migrated")
	}
	if dirExists(want) {
		t.Fatalf("scoped dir should not exist yet (no migrate)")
	}
}

func TestDefaultHarnessObjectDirBesideDB_migratesNonTempLegacy(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "testdata", "harness_migrate_"+t.Name())
	if isTempParent(root) {
		t.Skip("module path classified as temp")
	}
	_ = os.RemoveAll(root)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	db := filepath.Join(root, "fuzz.db")
	if err := os.WriteFile(db, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "harness")
	if err := os.MkdirAll(filepath.Join(legacy, "ab"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, "ab", "marker"), []byte("blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := DefaultHarnessObjectDirBesideDB(db)
	want := db + ".harness"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(want, "ab", "marker")); err != nil {
		t.Fatalf("migrated blob missing: %v", err)
	}
}
