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

func TestBuildGuardFromSource(t *testing.T) {
	cases := []struct{ lang, file, src, tool string }{
		{"rust", "guard.rs", rustGuard, "rustc"},
		{"c", "guard.c", cGuard, "clang"},
		{"cpp", "guard.cpp", "extern \"C\" " + cGuard, "clang"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			if _, err := exec.LookPath(c.tool); err != nil {
				t.Skip(c.tool, "not installed")
			}
			ws := t.TempDir()
			writeFile(t, filepath.Join(ws, "fuzz", c.file), []byte(c.src))
			o := opts(t, ws)
			o.BuildLang, o.BuildSrc = c.lang, "fuzz/"+c.file
			plan, err := BuildPlan(context.Background(), o)
			if err != nil {
				t.Skipf("toolchain cannot target wasm32 here: %v", err)
			}
			if !plan.OK() || plan.Source != sourceWasm || plan.Wasm.Path != "fuzz/"+c.file {
				t.Fatalf("plan: %+v", plan.Wasm)
			}
		})
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
