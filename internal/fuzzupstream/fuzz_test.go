package fuzzupstream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBuildAllTargets(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
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
