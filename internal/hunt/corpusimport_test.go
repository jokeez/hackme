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
