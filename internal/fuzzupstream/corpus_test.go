package fuzzupstream

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCorpusKeyStable(t *testing.T) {
	a := corpusKey([]byte("hello"))
	b := corpusKey([]byte("hello"))
	if a != b || a == "" {
		t.Fatalf("unstable key %q %q", a, b)
	}
	if corpusKey([]byte("hello")) == corpusKey([]byte("world")) {
		t.Fatal("collision")
	}
}

func TestAddAndLoadCorpusDir(t *testing.T) {
	dir := t.TempDir()
	saved := 0
	if !addCorpusFile(dir, []byte(`{"a":1}`), &saved, 64) {
		t.Fatal("expected save")
	}
	if saved != 1 {
		t.Fatalf("saved=%d", saved)
	}
	// duplicate should not save again
	if addCorpusFile(dir, []byte(`{"a":1}`), &saved, 64) {
		t.Fatal("duplicate should skip")
	}
	got := loadCorpusDir(dir, 10)
	if len(got) != 1 {
		t.Fatalf("load=%d", len(got))
	}
}

func TestShouldKeepNovel(t *testing.T) {
	lengths := map[int]int{}
	seen := map[string]struct{}{}
	in := []byte("abcdef")
	if !shouldKeepNovel(in, lengths, seen, 47) {
		t.Fatal("expected keep on first sample")
	}
	seen[corpusKey(in)] = struct{}{}
	if shouldKeepNovel(in, lengths, seen, 94) {
		t.Fatal("seen hash should skip")
	}
}

func TestLoadExtraSeedDirs(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, ".cache", "hunt-lf-seeds", "mpack")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	dirs := loadExtraSeedDirs(root, "mpack")
	if len(dirs) != 1 || dirs[0] != d {
		t.Fatalf("dirs=%v", dirs)
	}
}

func TestLoadCorpusDirSkipsSymlinks(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(t.TempDir(), "secret.bin")
	if err := os.WriteFile(secret, []byte("top-secret-seed-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "seed-real.bin"), []byte("real-seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "seed-link.bin")); err != nil {
		t.Fatal(err)
	}
	got := loadCorpusDir(dir, 10)
	if len(got) != 1 || string(got[0]) != "real-seed" {
		t.Fatalf("want only regular seed, got %#v", got)
	}
}

func TestLoadExtraSeedDirsSkipsSymlinkRoot(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(t.TempDir(), "real-corpus")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	linkParent := filepath.Join(root, ".cache", "hunt-lf-seeds")
	if err := os.MkdirAll(linkParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(linkParent, "mpack")); err != nil {
		t.Fatal(err)
	}
	if dirs := loadExtraSeedDirs(root, "mpack"); len(dirs) != 0 {
		t.Fatalf("symlink corpus root must be refused, got %v", dirs)
	}
}

func TestShareWallFromEnv(t *testing.T) {
	t.Setenv("HACKME_OSS_SHARE_WALL", "")
	if ShareWallFromEnv() {
		t.Fatal("empty should be false")
	}
	t.Setenv("HACKME_OSS_SHARE_WALL", "1")
	if !ShareWallFromEnv() {
		t.Fatal("1 should be true")
	}
}
