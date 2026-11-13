package main

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	rustGuard = "#[no_mangle]\npub extern \"C\" fn check(x: i64) -> i32 { (x == 42) as i32 }\n"
	cGuard    = "int check(long long x) { return x == 42; }\n"
)

// requireWasmTarget skips only when the toolchain itself can't target wasm32;
// once it can, any BuildPlan error is a real failure.
func requireWasmTarget(t *testing.T, tool string) {
	t.Helper()
	if _, err := exec.LookPath(tool); err != nil {
		t.Skip(tool, "not installed")
	}
	dir := t.TempDir()
	var cmd *exec.Cmd
	switch tool {
	case "rustc":
		src := filepath.Join(dir, "probe.rs")
		writeFile(t, src, []byte("#![no_std]\n#[panic_handler]\nfn p(_: &core::panic::PanicInfo) -> ! { loop {} }\n"))
		cmd = exec.Command("rustc", "--target", "wasm32-unknown-unknown", "--crate-type=cdylib", "-o", filepath.Join(dir, "p.wasm"), src)
	case "clang":
		src := filepath.Join(dir, "probe.c")
		writeFile(t, src, []byte("int f(void) { return 0; }\n"))
		cmd = exec.Command("clang", "--target=wasm32", "-nostdlib", "-Wl,--no-entry", "-o", filepath.Join(dir, "p.wasm"), src)
	default:
		t.Fatalf("unknown tool %s", tool)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("%s cannot target wasm32 here: %v %s", tool, err, out)
	}
}

func TestBuildGuardFromSource(t *testing.T) {
	cases := []struct{ name, lang, file, src, tool string }{
		{"rust", "rust", "guard.rs", rustGuard, "rustc"},
		{"c", "c", "guard.c", cGuard, "clang"},
		{"cpp", "cpp", "guard.cpp", "extern \"C\" " + cGuard, "clang"},
		// Byte guards export only check_bytes (+ memory); they must link.
		{"c check_bytes only", "c", "bytes.c", "int check_bytes(unsigned char *p, int n) { return n > 3 && p[0] == 1; }\n", "clang"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			requireWasmTarget(t, c.tool)
			ws := t.TempDir()
			writeFile(t, filepath.Join(ws, "fuzz", c.file), []byte(c.src))
			o := opts(t, ws)
			o.BuildLang, o.BuildSrc = c.lang, "fuzz/"+c.file
			plan, err := BuildPlan(context.Background(), o)
			if err != nil {
				t.Fatalf("BuildPlan: %v", err)
			}
			if !plan.OK() || plan.Source != sourceWasm || plan.Wasm.Path != "fuzz/"+c.file {
				t.Fatalf("plan: %+v", plan.Wasm)
			}
		})
	}
}

// A guard with neither export must reach the sandbox gate (clear ABI error),
// not die in the linker.
func TestBuildGuardWithoutExportsHitsGate(t *testing.T) {
	requireWasmTarget(t, "clang")
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "g.c"), []byte("int helper(int x) { return x + 1; }\n"))
	o := opts(t, ws)
	o.BuildLang, o.BuildSrc = "c", "g.c"
	plan, err := BuildPlan(context.Background(), o)
	if err != nil {
		t.Fatalf("want a plan rejected by the gate, got build error: %v", err)
	}
	if plan.OK() || plan.Wasm.Error == "" {
		t.Fatalf("wasm without check/check_bytes must be rejected: %+v", plan.Wasm)
	}
}

func TestBuildUsageErrors(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "g.rs"), []byte(rustGuard))
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "g.rs"), []byte(rustGuard))
	writeFile(t, filepath.Join(ws, "g.wasm"), minimalCheckWasm)
	cases := map[string]func(*Options){
		"lang only":      func(o *Options) { o.BuildLang = "rust" },
		"source only":    func(o *Options) { o.BuildSrc = "g.rs" },
		"wasm+source":    func(o *Options) { o.BuildLang, o.BuildSrc, o.WasmPath = "rust", "g.rs", "g.wasm" },
		"preset+source":  func(o *Options) { o.BuildLang, o.BuildSrc, o.DigPreset = "rust", "g.rs", "secrets" },
		"bad language":   func(o *Options) { o.BuildLang, o.BuildSrc = "python", "g.rs" },
		"source outside": func(o *Options) { o.BuildLang, o.BuildSrc = "rust", filepath.Join(outside, "g.rs") },
		"source dotdot":  func(o *Options) { o.BuildLang, o.BuildSrc = "rust", "../x.rs" },
		"source is dir":  func(o *Options) { o.BuildLang, o.BuildSrc = "rust", "." },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			o := opts(t, ws)
			mut(&o)
			if _, err := BuildPlan(context.Background(), o); !errors.Is(err, errUsage) {
				t.Fatalf("want usage error, got %v", err)
			}
		})
	}
}

func TestBuildCompileErrorIsNotUsage(t *testing.T) {
	if _, err := exec.LookPath("rustc"); err != nil {
		t.Skip("rustc not installed")
	}
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "bad.rs"), []byte("this is not rust"))
	o := opts(t, ws)
	o.BuildLang, o.BuildSrc = "rust", "bad.rs"
	_, err := BuildPlan(context.Background(), o)
	if err == nil || errors.Is(err, errUsage) {
		t.Fatalf("compile error should be a plain failure, got %v", err)
	}
}
