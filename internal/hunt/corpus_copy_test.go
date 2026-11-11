package hunt

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCopyCorpusFilesHardlinkSameFS(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	payload := []byte("seed-body-aaaa")
	if err := os.WriteFile(filepath.Join(src, "id:000001"), payload, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "id:000002"), []byte("seed-body-bbbb"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := copyCorpusFiles(src, dst, 32)
	if n != 2 {
		t.Fatalf("copied=%d want 2", n)
	}
	dest := filepath.Join(dst, "afl-id:000001")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("content=%q", got)
	}
	stSrc, err := os.Stat(filepath.Join(src, "id:000001"))
	if err != nil {
		t.Fatal(err)
	}
	stDst, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	// On Unix same-FS hardlink: same inode. Windows / cross-device falls back to copy.
	if runtime.GOOS != "windows" {
		if si, ok := fileInode(stSrc); ok {
			if di, ok2 := fileInode(stDst); ok2 && si != di {
				t.Fatalf("expected hardlink same inode got src=%d dst=%d", si, di)
			}
		}
	}
	mode := stDst.Mode().Perm()
	if mode&0o777 != 0o600 {
		t.Fatalf("dest mode=%o want 0600", mode)
	}
}

func TestCopyCorpusFilesSkipsExistingAndOversized(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "keep"), []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, (1<<20)+8)
	if err := os.WriteFile(filepath.Join(src, "huge"), big, 0o600); err != nil {
		t.Fatal(err)
	}
	pre := filepath.Join(dst, "afl-keep")
	if err := os.WriteFile(pre, []byte("already"), 0o600); err != nil {
		t.Fatal(err)
	}
	n := copyCorpusFiles(src, dst, 32)
	if n != 0 {
		t.Fatalf("existing dest must skip; oversized ignored; got n=%d", n)
	}
	got, _ := os.ReadFile(pre)
	if string(got) != "already" {
		t.Fatalf("must not overwrite existing: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "afl-huge")); err == nil {
		t.Fatal("oversized file must not be imported")
	}
}

func TestCopyCorpusFilesRespectsMaxAndStableOrder(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	for _, name := range []string{"c", "a", "b"} {
		if err := os.WriteFile(filepath.Join(src, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	n := copyCorpusFiles(src, dst, 2)
	if n != 2 {
		t.Fatalf("n=%d want 2", n)
	}
	// Lexicographic: a, b (c dropped by max).
	if _, err := os.Stat(filepath.Join(dst, "afl-a")); err != nil {
		t.Fatal("want afl-a")
	}
	if _, err := os.Stat(filepath.Join(dst, "afl-b")); err != nil {
		t.Fatal("want afl-b")
	}
	if _, err := os.Stat(filepath.Join(dst, "afl-c")); err == nil {
		t.Fatal("c must be beyond max")
	}
}

func TestCopyCorpusFilesCrashPrefix(t *testing.T) {
	src := t.TempDir()
	// Parent path contains "crashes" so names get afl-crash- prefix.
	crashDir := filepath.Join(src, "crashes")
	if err := os.MkdirAll(crashDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := os.WriteFile(filepath.Join(crashDir, "id:crash1"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	n := copyCorpusFiles(crashDir, dst, 8)
	if n != 1 {
		t.Fatalf("n=%d", n)
	}
	if _, err := os.Stat(filepath.Join(dst, "afl-crash-id:crash1")); err != nil {
		t.Fatal(err)
	}
}

func TestPlaceCorpusFileFallbackCopy(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	srcFile := filepath.Join(src, "seed")
	if err := os.WriteFile(srcFile, []byte("body"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dst, "out")
	if !placeCorpusFile(srcFile, dest) {
		t.Fatal("place failed")
	}
	got, err := os.ReadFile(dest)
	if err != nil || string(got) != "body" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

// High (fimasw): Hunt corpus must not follow harness-planted symlinks into host secrets.
func TestCopyCorpusFilesRefusesSymlinkToHostSecret(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	host := t.TempDir()
	secretPath := filepath.Join(host, ".env.vps")
	secret := []byte("HACKME_ADMIN_TOKEN=super-secret-value\n")
	if err := os.WriteFile(secretPath, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	dst := t.TempDir()
	if err := os.Symlink(secretPath, filepath.Join(src, "leak")); err != nil {
		t.Fatal(err)
	}
	// Legitimate seed alongside the decoy.
	if err := os.WriteFile(filepath.Join(src, "ok"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	n := copyCorpusFiles(src, dst, 32)
	if n != 1 {
		t.Fatalf("copied=%d want 1 (symlink skipped)", n)
	}
	if _, err := os.Lstat(filepath.Join(dst, "afl-leak")); err == nil {
		t.Fatal("symlink must not be copied/linked into destination")
	}
	got, err := os.ReadFile(filepath.Join(dst, "afl-ok"))
	if err != nil || string(got) != "seed" {
		t.Fatalf("regular seed missing: %q err=%v", got, err)
	}
	if placeCorpusFile(filepath.Join(src, "leak"), filepath.Join(dst, "placed")) {
		t.Fatal("placeCorpusFile must refuse symlink source")
	}
}

func TestReadRegularCorpusFileRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink semantics differ on windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "secret")
	if err := os.WriteFile(target, []byte("SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "leak")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRegularCorpusFile(link, 65536); err == nil {
		t.Fatal("expected refuse symlink")
	}
	b, err := ReadRegularCorpusFile(target, 65536)
	if err != nil || string(b) != "SECRET" {
		t.Fatalf("regular read: %q err=%v", b, err)
	}
}
