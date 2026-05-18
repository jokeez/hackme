package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMergePathListDedupes(t *testing.T) {
	got := mergePathList("/a:/b", "/b:/c")
	want := "/a" + string(os.PathListSeparator) + "/b" + string(os.PathListSeparator) + "/c"
	if got != want {
		t.Fatalf("mergePathList() = %q want %q", got, want)
	}
}

func TestMergeToolchainEnvFilePATH(t *testing.T) {
	dir := t.TempDir()
	envf := filepath.Join(dir, ".env.toolchains")
	if err := os.WriteFile(envf, []byte("PATH=/zig:/tinygo\nRUSTUP_HOME=/rustup\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "/usr/bin")
	t.Setenv("RUSTUP_HOME", "")
	os.Unsetenv("RUSTUP_HOME")

	if err := mergeToolchainEnvFile(envf); err != nil {
		t.Fatal(err)
	}
	path := os.Getenv("PATH")
	if !strings.HasPrefix(path, "/zig") {
		t.Fatalf("PATH should start with toolchain dirs, got %q", path)
	}
	if !strings.Contains(path, "/usr/bin") {
		t.Fatalf("PATH should retain existing entries, got %q", path)
	}
	if got := os.Getenv("RUSTUP_HOME"); got != "/rustup" {
		t.Fatalf("RUSTUP_HOME = %q want /rustup", got)
	}
}
