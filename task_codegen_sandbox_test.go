package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWrapCompilerCmdRequireSandboxFailClosed(t *testing.T) {
	t.Setenv("HACKME_FROM_CODE_REQUIRE_SANDBOX", "1")
	t.Setenv("PATH", t.TempDir()) // empty PATH: no bwrap/nsjail
	inner := exec.CommandContext(context.Background(), "true")
	work := t.TempDir()
	out := filepath.Join(work, "out.wasm")
	_, err := wrapCompilerCmd(context.Background(), work, out, inner)
	if err == nil {
		t.Fatal("expected fail-closed when REQUIRE_SANDBOX set without bwrap/nsjail")
	}
	if !strings.Contains(err.Error(), "HACKME_FROM_CODE_REQUIRE_SANDBOX") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWrapCompilerCmdAllowsHostWhenNotRequired(t *testing.T) {
	t.Setenv("HACKME_FROM_CODE_REQUIRE_SANDBOX", "0")
	t.Setenv("PATH", t.TempDir())
	inner := exec.CommandContext(context.Background(), "true")
	work := t.TempDir()
	out := filepath.Join(work, "out.wasm")
	got, err := wrapCompilerCmd(context.Background(), work, out, inner)
	if err != nil {
		t.Fatal(err)
	}
	if got != inner {
		t.Fatal("without sandbox binary and REQUIRE=0, should return inner command")
	}
}

func TestWrapCompilerCmdUsesBwrapWhenAvailable(t *testing.T) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		t.Skip("bwrap not installed")
	}
	t.Setenv("HACKME_FROM_CODE_REQUIRE_SANDBOX", "1")
	// Keep real PATH so LookPath finds system bwrap.
	if p := os.Getenv("PATH"); p == "" {
		t.Fatal("PATH empty")
	}
	inner := exec.CommandContext(context.Background(), "true")
	inner.Dir = t.TempDir()
	work := inner.Dir
	out := filepath.Join(work, "out.wasm")
	got, err := wrapCompilerCmd(context.Background(), work, out, inner)
	if err != nil {
		t.Fatal(err)
	}
	if got == inner {
		t.Fatal("expected bwrap-wrapped command")
	}
	if !strings.Contains(got.Path, "bwrap") && filepath.Base(got.Path) != "bwrap" {
		// Path may be absolute
		if filepath.Base(got.Args[0]) != "bwrap" && filepath.Base(got.Path) != "bwrap" {
			t.Fatalf("want bwrap wrapper, path=%q args0=%q", got.Path, got.Args[0])
		}
	}
	joined := strings.Join(got.Args, " ")
	if !strings.Contains(joined, "--unshare-net") {
		t.Fatalf("bwrap args missing unshare-net: %v", got.Args)
	}
}
