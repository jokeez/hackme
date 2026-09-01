package fuzzingcli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDigSeedFilesFilters(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "crash-1.bin"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadDigSeedFiles(dir, 8)
	if err != nil || len(got) != 1 || string(got[0]) != "x" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestMergeDigSeedCorpusU64(t *testing.T) {
	dir := t.TempDir()
	pack := "script_bounds"
	seedDir := DigSeedDir(dir, pack)
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "s.bin"), []byte{0x4c, 0x01, 0x02}, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"input_mode": "u64", "seed_corpus": []any{uint64(1)}}
	n, err := MergeDigSeedCorpus(cfg, dir, pack)
	if err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}
