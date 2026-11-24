package fuzzupstream

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildTargetCacheHitSkipsClone(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", t.TempDir())
	tgt := Target{ID: "cjson", Repo: "https://invalid.example/no-such-repo", Ref: "main", Driver: "nope.c"}
	bin := asanCachedBinPath(root, tgt, "address,undefined", "")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("cached-asan"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, _, err := BuildTarget(context.Background(), root, tgt)
	if err != nil || got != bin {
		t.Fatalf("got %s err %v", got, err)
	}
	afl := aflCachedBinPath(root, tgt)
	if err := os.MkdirAll(filepath.Dir(afl), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(afl, []byte("cached-afl"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, _, err = BuildTargetAFL(context.Background(), root, tgt)
	if err != nil || got != afl {
		t.Fatalf("afl got %s err %v", got, err)
	}
}

func TestRegularCachedBinRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "cache.bin")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if regularCachedBin(link) {
		t.Fatal("symlink must not be a cached binary")
	}
	if err := writeCachedBin(link, []byte("fuzzer")); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(outside)
	if err != nil || string(body) != "secret" {
		t.Fatalf("write followed symlink: %q err=%v", body, err)
	}
	st, err := os.Lstat(link)
	if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		t.Fatalf("cache mode=%v err=%v", st.Mode(), err)
	}
}
