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
	// Must not whole-root RO bind (include_str!("/etc/...") exfil).
	if strings.Contains(joined, "--ro-bind / /") || strings.Contains(joined, "--ro-bind-try / /") {
		t.Fatalf("sandbox must not bind whole root: %v", got.Args)
	}
	if !strings.Contains(joined, "/usr") {
		t.Fatalf("expected narrow /usr bind: %v", got.Args)
	}
	if !strings.Contains(joined, "--bind "+work) && !strings.Contains(joined, work) {
		t.Fatalf("expected writable workdir bind: %v", got.Args)
	}
}

func TestFromCodeEnabledEnvAndBindDefault(t *testing.T) {
	t.Setenv("HACKME_FROM_CODE", "0")
	if fromCodeEnabled() {
		t.Fatal("HACKME_FROM_CODE=0 must disable")
	}
	t.Setenv("HACKME_FROM_CODE", "1")
	if !fromCodeEnabled() {
		t.Fatal("HACKME_FROM_CODE=1 must enable")
	}
	t.Setenv("HACKME_FROM_CODE", "")
	t.Setenv("HACKME_BIND_ADDR", "127.0.0.1:8080")
	if !fromCodeEnabled() {
		t.Fatal("loopback bind default should enable from_code")
	}
	t.Setenv("HACKME_BIND_ADDR", "0.0.0.0:8080")
	if fromCodeEnabled() {
		t.Fatal("public bind default should disable from_code")
	}
}

func TestCompilerSandboxROBindsExcludesEtcRoot(t *testing.T) {
	inner := exec.Command("true")
	inner.Env = append(os.Environ(), "RUSTUP_HOME=/tmp/fake-rustup-does-not-need-exist")
	binds := compilerSandboxROBinds(inner, "true")
	for _, b := range binds {
		if b == "/etc" || b == "/" {
			t.Fatalf("must not bind %q", b)
		}
	}
	joined := strings.Join(binds, " ")
	if !strings.Contains(joined, "/usr") && pathExists("/usr") {
		t.Fatalf("expected /usr in binds: %v", binds)
	}
}

func TestLooksLikeToolchainDir(t *testing.T) {
	if !looksLikeToolchainDir("/opt/hackme/.cargo/bin") {
		t.Fatal("expected cargo bin as toolchain")
	}
	if looksLikeToolchainDir("/home") || looksLikeToolchainDir("/etc") {
		t.Fatal("broad roots must not count as toolchain")
	}
}
