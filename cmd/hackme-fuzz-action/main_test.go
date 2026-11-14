package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/fuzzingcli"
	"hackme/internal/sandbox"
)

// minimalCheckWasm exports check(i64)->i32 returning 0.
var minimalCheckWasm = []byte{
	0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
	0x01, 0x06, 0x01, 0x60, 0x01, 0x7e, 0x01, 0x7f, // type (i64)->i32
	0x03, 0x02, 0x01, 0x00, // func 0 : type 0
	0x07, 0x09, 0x01, 0x05, 'c', 'h', 'e', 'c', 'k', 0x00, 0x00, // export "check"
	0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00, 0x0b, // i32.const 0
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func opts(t *testing.T, ws string) Options {
	t.Helper()
	return Options{Workspace: ws, HackmeRoot: fuzzingcli.DiscoverRepoRoot(), TmpDir: t.TempDir()}
}

func TestCustomWasmPlan(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "out", "guard.wasm"), minimalCheckWasm)
	o := opts(t, ws)
	o.WasmPath = "out/guard.wasm"
	o.Package = "audit"
	plan, err := BuildPlan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !plan.OK() {
		t.Fatalf("minimal wasm rejected: %s", plan.Wasm.Error)
	}
	if plan.Source != sourceWasm || plan.DigPackage != "audit" {
		t.Fatalf("source=%s package=%s", plan.Source, plan.DigPackage)
	}
	hexRef, _ := plan.Payload["wasm_check_hex"].(string)
	if !strings.Contains(hexRef, plan.Wasm.SHA256) || strings.Contains(hexRef, "0061736d") {
		t.Fatalf("payload must reference wasm by hash, not embed it: %q", hexRef)
	}
	if _, ok := plan.Payload["dig_preset"]; ok {
		t.Fatal("custom wasm plan must not carry dig_preset")
	}
}

func TestPresetPlan(t *testing.T) {
	o := opts(t, t.TempDir())
	o.DigPreset = "smoke" // alias of scan_smoke
	// scan_smoke's pack ships only Rust source, so it is built on the fly.
	requireWasmTarget(t, "rustc")
	plan, err := BuildPlan(context.Background(), o)
	if err != nil {
		t.Fatalf("BuildPlan: %v", err)
	}
	if !plan.OK() {
		t.Fatalf("shipped preset rejected: %s", plan.Wasm.Error)
	}
	if plan.DigPreset != "scan_smoke" || plan.GuardPack != "overflow_smoke" || plan.DigPackage != "scan" {
		t.Fatalf("got preset=%s pack=%s package=%s", plan.DigPreset, plan.GuardPack, plan.DigPackage)
	}
	if plan.Payload["dig_preset"] != "scan_smoke" {
		t.Fatalf("payload dig_preset=%v", plan.Payload["dig_preset"])
	}
}

func TestUsageErrors(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "g.wasm"), minimalCheckWasm)
	cases := map[string]func(*Options){
		"nothing":         func(o *Options) {},
		"bad mode":        func(o *Options) { o.Mode = "publish"; o.WasmPath = "g.wasm" },
		"wasm+preset":     func(o *Options) { o.WasmPath = "g.wasm"; o.DigPreset = "secrets" },
		"unknown preset":  func(o *Options) { o.DigPreset = "nope" },
		"unknown pack":    func(o *Options) { o.WasmPath = "g.wasm"; o.GuardPack = "nope" },
		"unknown package": func(o *Options) { o.WasmPath = "g.wasm"; o.Package = "mega" },
		"preset/pack mismatch": func(o *Options) {
			o.DigPreset = "scan_smoke"
			o.GuardPack = "secrets"
		},
		"missing wasm": func(o *Options) { o.WasmPath = "nope.wasm" },
		"dotdot":       func(o *Options) { o.WasmPath = "../g.wasm" },
		"absolute out": func(o *Options) { o.WasmPath = "/etc/hostname" },
		"directory":    func(o *Options) { o.WasmPath = "." },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			o := opts(t, ws)
			mut(&o)
			_, err := BuildPlan(context.Background(), o)
			if !errors.Is(err, errUsage) {
				t.Fatalf("want usage error, got %v", err)
			}
		})
	}
}

func TestSymlinkEscapeRefused(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.wasm"), minimalCheckWasm)
	ws := t.TempDir()
	if err := os.Symlink(filepath.Join(outside, "secret.wasm"), filepath.Join(ws, "guard.wasm")); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if err := os.Symlink(outside, filepath.Join(ws, "dir")); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"guard.wasm", "dir/secret.wasm"} {
		o := opts(t, ws)
		o.WasmPath = p
		if _, err := BuildPlan(context.Background(), o); !errors.Is(err, errUsage) || !strings.Contains(err.Error(), "outside") {
			t.Fatalf("%s: want outside-root refusal, got %v", p, err)
		}
	}
}

func TestOversizeRefused(t *testing.T) {
	ws := t.TempDir()
	big := make([]byte, sandbox.MaxCheckWasmBytes()+1)
	copy(big, minimalCheckWasm)
	writeFile(t, filepath.Join(ws, "big.wasm"), big)
	o := opts(t, ws)
	o.WasmPath = "big.wasm"
	if _, err := BuildPlan(context.Background(), o); !errors.Is(err, errUsage) || !strings.Contains(err.Error(), "cap") {
		t.Fatalf("want size cap refusal, got %v", err)
	}
}

func TestRunExitCodes(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "ok.wasm"), minimalCheckWasm)
	writeFile(t, filepath.Join(ws, "bad.wasm"), []byte("\x00asm\x01\x00\x00\x00garbage"))
	root := fuzzingcli.DiscoverRepoRoot()
	planOut := filepath.Join(t.TempDir(), "plan.json")

	var out, errb bytes.Buffer
	if code := run([]string{"-workspace", ws, "-hackme-root", root, "-wasm", "ok.wasm", "-plan-out", planOut}, &out, &errb); code != 0 {
		t.Fatalf("ok.wasm exit=%d stderr=%s", code, errb.String())
	}
	var p Plan
	if err := json.Unmarshal(out.Bytes(), &p); err != nil || !p.Wasm.Valid {
		t.Fatalf("stdout plan: %v %+v", err, p)
	}
	if b, err := os.ReadFile(planOut); err != nil || !bytes.Equal(b, out.Bytes()) {
		t.Fatalf("plan-out mismatch: %v", err)
	}

	out.Reset()
	errb.Reset()
	if code := run([]string{"-workspace", ws, "-hackme-root", root, "-wasm", "bad.wasm"}, &out, &errb); code != 1 {
		t.Fatalf("bad.wasm exit=%d, want 1", code)
	}
	if err := json.Unmarshal(out.Bytes(), &p); err != nil || p.Wasm.Valid || p.Wasm.Error == "" {
		t.Fatalf("rejected wasm should still print a plan with the reason: %v %+v", err, p.Wasm)
	}

	if code := run([]string{"-workspace", ws, "stray"}, &out, &errb); code != 2 {
		t.Fatalf("stray arg exit=%d, want 2", code)
	}
}

func TestCleanTitle(t *testing.T) {
	got := cleanTitle("  PR #7\n<!-- x -->‮\t", fuzzingcli.GuardPack{}, false)
	if strings.ContainsAny(got, "\n\t‮") {
		t.Fatalf("control chars kept: %q", got)
	}
	if n := len([]rune(cleanTitle(strings.Repeat("я", 500), fuzzingcli.GuardPack{}, false))); n > maxTitleRunes {
		t.Fatalf("title not capped: %d", n)
	}
	if cleanTitle(" \n ", fuzzingcli.GuardPack{}, false) == "" {
		t.Fatal("empty title should fall back")
	}
}

// The payload must carry the effective mutation_rounds: AdjustPackageForPack
// keeps the larger of package and pack, the pack config must not undercut it.
func TestDeepKeepsEffectiveMutationRounds(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "g.wasm"), minimalCheckWasm)
	for _, pkgName := range []string{"scan", "audit", "deep"} {
		o := opts(t, ws)
		o.WasmPath, o.GuardPack, o.Package = "g.wasm", "overflow_smoke", pkgName
		plan, err := BuildPlan(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		pack, _ := fuzzingcli.GuardPackFor("overflow_smoke")
		base, _ := fuzzingcli.B2BPackageFor(pkgName)
		want := fuzzingcli.AdjustPackageForPack(base, pack).MutationRounds
		if got := plan.Payload["mutation_rounds"]; want > 0 && got != want {
			t.Errorf("%s: payload mutation_rounds=%v, effective=%d", pkgName, got, want)
		}
	}
}

// Mirrors the node's customer_guard rule (main 8647a3c9): own wasm, even
// labelled with a guard_pack, fails the gate on violations; only the
// byte-identical shipped pack module is a detector pack.
func TestPlanCustomerGuard(t *testing.T) {
	ws := t.TempDir()
	writeFile(t, filepath.Join(ws, "g.wasm"), minimalCheckWasm)
	for _, pack := range []string{"", "overflow_smoke"} {
		o := opts(t, ws)
		o.WasmPath, o.GuardPack = "g.wasm", pack
		plan, err := BuildPlan(context.Background(), o)
		if err != nil {
			t.Fatal(err)
		}
		if !plan.CustomerGuard || plan.Payload["customer_guard"] != true {
			t.Errorf("pack %q: own wasm must be a customer guard", pack)
		}
	}

	root := fuzzingcli.DiscoverRepoRoot()
	pack, _ := fuzzingcli.GuardPackFor("overflow_smoke")
	shipped, err := os.ReadFile(filepath.Join(root, pack.WasmRelPath))
	if err != nil {
		t.Skip("overflow_smoke wasm not built here (scripts/ops/build_scan_smoke_guards.sh)")
	}
	writeFile(t, filepath.Join(ws, "same.wasm"), shipped)
	o := opts(t, ws)
	o.WasmPath, o.GuardPack = "same.wasm", "overflow_smoke"
	plan, err := BuildPlan(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if plan.CustomerGuard {
		t.Fatal("the shipped pack module itself is a detector pack, not a customer guard")
	}
}
