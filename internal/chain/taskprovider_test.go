package chain

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInternalTaskProvider(t *testing.T) {
	var p InternalTaskProvider
	s, err := p.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "internal_synthetic_poh" || s.Source != "internal" {
		t.Fatalf("%+v", s)
	}
}

func TestFileTaskProvider_fallbackEmptyDir(t *testing.T) {
	dir := t.TempDir()
	p := NewFileTaskProvider(dir, InternalTaskProvider{})
	s, err := p.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != "internal" {
		t.Fatalf("want internal fallback, got %+v", s)
	}
}

func TestFileTaskProvider_loadsNewestJSON(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.json")
	newPath := filepath.Join(dir, "new.json")
	if err := os.WriteFile(oldPath, []byte(`{"id":"task-old","kind":"synthetic_poh_v1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(15 * time.Millisecond)
	if err := os.WriteFile(newPath, []byte(`{"id":"task-new","kind":"synthetic_poh_v1","artifact_hash":"abc","reward_hmc":0.02,"timeout_ms":3000}`), 0o644); err != nil {
		t.Fatal(err)
	}

	p := NewFileTaskProvider(dir, InternalTaskProvider{})
	p.TTL = 0
	s, err := p.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "task-new" || s.Source != "file" {
		t.Fatalf("%+v", s)
	}
	if s.ArtifactHash != "abc" || s.RewardHMC != 0.02 || s.Timeout != 3*time.Second {
		t.Fatalf("%+v", s)
	}
	if s.ManifestPath != newPath {
		t.Fatalf("path %q", s.ManifestPath)
	}
}

func TestFileTaskProvider_skipsUnderscorePrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "_skip.json"), []byte(`{"id":"x"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	p := NewFileTaskProvider(dir, InternalTaskProvider{})
	p.TTL = 0
	s, err := p.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Source != "internal" {
		t.Fatalf("expected fallback, got %+v", s)
	}
}
