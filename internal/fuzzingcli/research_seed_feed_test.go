package fuzzingcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPoolSeedFromResearchEnabledDefaultOff(t *testing.T) {
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "")
	if PoolSeedFromResearchEnabled() {
		t.Fatal("expected default off")
	}
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "0")
	if PoolSeedFromResearchEnabled() {
		t.Fatal("expected 0 off")
	}
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "1")
	if !PoolSeedFromResearchEnabled() {
		t.Fatal("expected 1 on")
	}
}

func TestMaybeFeedResearchSeedsToDigGated(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, ".cache", "hunt-lf-seeds", "mpack")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.bin"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "0")
	n, err := MaybeFeedResearchSeedsToDig(dir, "cfgpack_msgpack_guard", "mpack")
	if err != nil || n != 0 {
		t.Fatalf("gated feed n=%d err=%v", n, err)
	}
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "1")
	n, err = MaybeFeedResearchSeedsToDig(dir, "cfgpack_msgpack_guard", "mpack")
	if err != nil || n != 1 {
		t.Fatalf("enabled feed n=%d err=%v", n, err)
	}
	// Path traversal in research target id must be rejected.
	n, err = MaybeFeedResearchSeedsToDig(dir, "cfgpack_msgpack_guard", "../etc")
	if err != nil || n != 0 {
		t.Fatalf("traversal feed n=%d err=%v", n, err)
	}
}

// Low: research→dig feed must skip symlinks in hunt-lf-seeds trees.
func TestMaybeFeedResearchSeedsToDigSkipsSymlink(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, ".cache", "hunt-lf-seeds", "mpack")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.bin"), []byte("seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "secret.env")
	if err := os.WriteFile(secret, []byte("TOKEN=leak\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(src, "leak.bin")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HACKME_POOL_SEED_FROM_RESEARCH", "1")
	n, err := MaybeFeedResearchSeedsToDig(dir, "cfgpack_msgpack_guard", "mpack")
	if err != nil || n != 1 {
		t.Fatalf("symlink must be skipped; n=%d err=%v", n, err)
	}
	dst := DigSeedDir(dir, "cfgpack_msgpack_guard")
	if _, err := os.Lstat(filepath.Join(dst, "research-leak.bin")); err == nil {
		t.Fatal("symlink seed must not be copied into dig cache")
	}
}
