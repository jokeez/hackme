package fuzzupstream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildAllTargets(t *testing.T) {
	root := repoRoot(t)
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tgt := range m.Targets {
		tgt := tgt
		t.Run(tgt.ID, func(t *testing.T) {
			t.Parallel()
			driverSrc := DriverSourcePath(root, tgt)
			if _, err := os.Stat(driverSrc); err != nil {
				t.Skipf("driver not in repo: %s", tgt.Driver)
			}
			if TargetLanguage(tgt) == "rust" {
				if !RustNightlyASANAvailable() {
					t.Skip("rustc +nightly unavailable")
				}
			} else if _, err := exec.LookPath("clang"); err != nil {
				t.Skip("clang not installed")
			}
			bin, clone, err := BuildTarget(ctx, root, tgt)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(bin); err != nil {
				t.Fatalf("binary: %v", err)
			}
			if _, err := os.Stat(clone); err != nil {
				t.Fatalf("clone: %v", err)
			}
		})
	}
}

func TestHuntSerdeJSONSmoke(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	if !RustNightlyASANAvailable() {
		t.Skip("rustc +nightly unavailable")
	}
	root := repoRoot(t)
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := m.TargetByID("serde_json")
	if err != nil {
		t.Skip("serde_json not in catalog")
	}
	if TargetLanguage(tgt) != "rust" {
		t.Fatalf("expected language=rust, got %q", tgt.Language)
	}
	ctx := context.Background()
	bin, _, err := BuildTarget(ctx, root, tgt)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Hunt(ctx, root, tgt, bin, seedsFromManifest(m), 500, 4096, 45)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Language != "rust" {
		t.Fatalf("report language=%q", rep.Language)
	}
	if rep.Iterations < 50 {
		t.Fatalf("expected iterations, got %d", rep.Iterations)
	}
	t.Logf("serde_json smoke: iterations=%d crashes=%d verdict=%s lang=%s", rep.Iterations, len(rep.Crashes), rep.Verdict, rep.Language)
}

func TestHuntMemchrSmoke(t *testing.T) {
	smokeRustTarget(t, "memchr", 300, 30)
}

func TestHuntQuickXMLSmoke(t *testing.T) {
	smokeRustTarget(t, "quick_xml", 300, 30)
}

func smokeRustTarget(t *testing.T, id string, budget, wallSec int) {
	t.Helper()
	if testing.Short() {
		t.Skip("short")
	}
	if !RustNightlyASANAvailable() {
		t.Skip("rustc +nightly unavailable")
	}
	root := repoRoot(t)
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := m.TargetByID(id)
	if err != nil {
		t.Skipf("%s not in catalog", id)
	}
	if TargetLanguage(tgt) != "rust" {
		t.Fatalf("%s language=%q want rust", id, tgt.Language)
	}
	ctx := context.Background()
	bin, _, err := BuildTarget(ctx, root, tgt)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Hunt(ctx, root, tgt, bin, seedsFromManifest(m), budget, 4096, wallSec)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Language != "rust" {
		t.Fatalf("report language=%q", rep.Language)
	}
	if rep.Iterations < 20 {
		t.Fatalf("expected iterations, got %d", rep.Iterations)
	}
	t.Logf("%s smoke: iterations=%d crashes=%d verdict=%s", id, rep.Iterations, len(rep.Crashes), rep.Verdict)
}

func TestTargetLanguage(t *testing.T) {
	if TargetLanguage(Target{}) != "c" {
		t.Fatal("default c")
	}
	if TargetLanguage(Target{Language: "Rust"}) != "rust" {
		t.Fatal("rust")
	}
}

func TestHuntJsmnSmoke(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	if testing.Short() {
		t.Skip("short")
	}
	root := repoRoot(t)
	m, err := LoadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := m.TargetByID("jsmn")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	bin, _, err := BuildTarget(ctx, root, tgt)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Hunt(ctx, root, tgt, bin, seedsFromManifest(m), 2000, 4096, 30)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Iterations < 100 {
		t.Fatalf("expected iterations, got %d", rep.Iterations)
	}
	t.Logf("jsmn smoke: iterations=%d crashes=%d verdict=%s", rep.Iterations, len(rep.Crashes), rep.Verdict)
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := wd
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("go.mod not found")
	return ""
}
