package hunt

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMergeLibFuzzerSeedCorpus(t *testing.T) {
	dir := t.TempDir()
	target := "jsmn"
	seedDir := filepath.Join(dir, ".cache", "hunt-lf-seeds", target)
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "a.bin"), []byte(`{"a":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seedDir, "crash-1.bin"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{}
	n, err := MergeLibFuzzerSeedCorpus(cfg, dir, target)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("merged=%d want 1", n)
	}
	if cfg["hunt_corpus_guided"] != true {
		t.Fatalf("expected local guided defaults: %+v", cfg)
	}
	n2, err := MergeLibFuzzerSeedCorpus(cfg, dir, target)
	if err != nil || n2 != 0 {
		t.Fatalf("second merge=%d err=%v", n2, err)
	}
}

func TestApplyHuntPowerScheduling(t *testing.T) {
	cfg := map[string]any{"power_mut_cap": 2}
	ApplyHuntPowerScheduling(cfg, "hunt_standard")
	if cfg["power_mut_cap"] != 10 {
		t.Fatalf("cap=%v", cfg["power_mut_cap"])
	}
}

func TestExportLibFuzzerSeedsSkipsInvalid(t *testing.T) {
	dir := t.TempDir()
	target := "export-test"
	n, err := ExportLibFuzzerSeeds(dir, target, [][]byte{nil, make([]byte, libFuzzerSeedMaxBytes+1), []byte("ok")})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("written=%d want 1", n)
	}
	loaded, err := LoadLibFuzzerSeedFiles(LibFuzzerSeedDir(dir, target), 0)
	if err != nil || len(loaded) != 1 || string(loaded[0]) != "ok" {
		t.Fatalf("loaded=%v err=%v", loaded, err)
	}
}

func TestLoadLibFuzzerSeedFilesFiltersCrash(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "crash-1.bin"), []byte("bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLibFuzzerSeedFiles(dir, 0)
	if err != nil || len(got) != 1 || string(got[0]) != "x" {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestLoadLibFuzzerSeedFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "good.bin"), []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "crash-1.bin"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	seeds, err := LoadLibFuzzerSeedFiles(dir, 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(seeds) != 1 || string(seeds[0]) != "ok" {
		t.Fatalf("seeds=%v", seeds)
	}
}

func TestExportLibFuzzerSeeds(t *testing.T) {
	dir := t.TempDir()
	target := "export-target"
	n, err := ExportLibFuzzerSeeds(dir, target, [][]byte{[]byte(`{"a":1}`), []byte("dup"), nil})
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("written=%d", n)
	}
	loaded, err := LoadLibFuzzerSeedFiles(LibFuzzerSeedDir(dir, target), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded=%d", len(loaded))
	}
}

func TestLibFuzzerSeedDir(t *testing.T) {
	got := LibFuzzerSeedDir("/repo", "jsmn")
	want := filepath.Join("/repo", ".cache", "hunt-lf-seeds", "jsmn")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
