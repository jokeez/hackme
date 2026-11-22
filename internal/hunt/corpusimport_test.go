package hunt

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
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

func TestRankLibFuzzerSeedsCaps(t *testing.T) {
	seeds := make([][]byte, 0, 100)
	for i := 0; i < 100; i++ {
		seeds = append(seeds, []byte{byte(i), byte(i ^ 0x5a), '{', '}'})
	}
	got := RankLibFuzzerSeeds(seeds, 16)
	if len(got) != 16 {
		t.Fatalf("cap: got %d want 16", len(got))
	}
	// Same inputs → same ranking (replay-ish stability for import).
	again := RankLibFuzzerSeeds(seeds, 16)
	if len(again) != 16 || string(got[0]) != string(again[0]) {
		t.Fatalf("ranking not stable")
	}
}

func TestLoadLibFuzzerSeedFilesPrefersNewestWhenCapped(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "old.bin")
	newPath := filepath.Join(dir, "new.bin")
	if err := os.WriteFile(oldPath, []byte("old-seed-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new-feature-unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(oldPath, past, past); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLibFuzzerSeedFiles(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || string(got[0]) != "new-feature-unit" {
		t.Fatalf("newest unit dropped by load cap: %#v", got)
	}
}

func TestLoadLibFuzzerSeedFilesKeepsDistinctLongPrefix(t *testing.T) {
	dir := t.TempDir()
	prefix := bytes.Repeat([]byte("A"), 32)
	old := append(append([]byte{}, prefix...), []byte("-old")...)
	fresh := append(append([]byte{}, prefix...), []byte("-fresh")...)
	if err := os.WriteFile(filepath.Join(dir, "old.bin"), old, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.bin"), fresh, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadLibFuzzerSeedFiles(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("shared 32-byte prefix dropped a distinct seed: %d", len(got))
	}
}

func TestLoadLibFuzzerSeedFilesEmptyDir(t *testing.T) {
	got, err := LoadLibFuzzerSeedFiles(t.TempDir(), 8)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("empty corpus loaded %d", len(got))
	}
}

func TestRankLibFuzzerSeedsPutsBootStartersAtTail(t *testing.T) {
	feature := []byte("feature-unit-unique-coverage")
	other := []byte("mid-coverage-unit-zz")
	boots := [][]byte{[]byte("{}"), []byte("[]"), []byte("null"), {0x00}, {0x80}}
	seeds := append(append([][]byte{}, boots...), feature, other)
	got := RankLibFuzzerSeeds(seeds, 20)
	if len(got) != 7 {
		t.Fatalf("got %d seeds", len(got))
	}
	if string(got[0]) != string(feature) || string(got[1]) != string(other) {
		t.Fatalf("fresh units not pinned ahead of boots: %#v", got)
	}
	tail := map[string]bool{}
	for _, b := range got[2:] {
		tail[string(b)] = true
	}
	for _, b := range boots {
		if !tail[string(b)] {
			t.Fatalf("boot %q missing from tail: %#v", b, got)
		}
	}
	// A one-byte unit that is not 0x00/0x80 is a real seed, not a starter.
	kept := RankLibFuzzerSeeds([][]byte{{0x01}, {0x00}}, 8)
	if len(kept) != 2 || kept[0][0] != 0x01 || kept[1][0] != 0x00 {
		t.Fatalf("0x01 must stay ahead of the 0x00 starter: %#v", kept)
	}
}

func TestRankLibFuzzerSeedsPinsFreshAheadOfBoots(t *testing.T) {
	feature := []byte("feature-unit-unique-coverage")
	seeds := make([][]byte, 0, 90)
	for i := 0; i < 10; i++ {
		seeds = append(seeds, []byte("{}"))
	}
	seeds = append(seeds, feature)
	for i := 0; i < 80; i++ {
		seeds = append(seeds, []byte{byte(i + 1), byte(i ^ 0xff)})
	}
	got := RankLibFuzzerSeeds(seeds, 8)
	if len(got) != 8 {
		t.Fatalf("cap: got %d", len(got))
	}
	found := false
	for _, b := range got {
		if string(b) == "{}" {
			t.Fatalf("boot outranked the fresh unit: %#v", got)
		}
		if string(b) == string(feature) {
			found = true
		}
	}
	if !found {
		t.Fatalf("rank cap dropped the fresh unit: %#v", got)
	}
}

func TestApplyHuntPowerScheduling(t *testing.T) {
	cfg := map[string]any{"power_mut_cap": 2}
	ApplyHuntPowerScheduling(cfg, "hunt_standard")
	if cfg["power_mut_cap"] != 16 {
		t.Fatalf("cap=%v", cfg["power_mut_cap"])
	}
	if cfg["havoc_deep_v210"] != true {
		t.Fatalf("standard v210=%v", cfg["havoc_deep_v210"])
	}
	lite := map[string]any{"power_mut_cap": 2}
	ApplyHuntPowerScheduling(lite, "hunt_lite")
	if _, ok := lite["havoc_deep_v210"]; ok {
		t.Fatalf("lite must not force v210, got %#v", lite["havoc_deep_v210"])
	}
	heavy := map[string]any{"power_mut_cap": 2}
	ApplyHuntPowerScheduling(heavy, "hunt_heavy")
	if heavy["power_mut_cap"] != 18 {
		t.Fatalf("heavy cap=%v want 18", heavy["power_mut_cap"])
	}
	off := map[string]any{"power_mut_cap": 2, "hunt_heavy_power_boost": false}
	ApplyHuntPowerScheduling(off, "hunt_heavy")
	if off["power_mut_cap"] != 14 {
		t.Fatalf("heavy boost off cap=%v want 14", off["power_mut_cap"])
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

func TestExportLibFuzzerSeedsRefusesSymlinkEscape(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	seedDir := LibFuzzerSeedDir(dir, "jsmn")
	if err := os.MkdirAll(seedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte("owned-by-miner")
	sum := sha256.Sum256(payload)
	name := fmt.Sprintf("lf-%s.bin", hex.EncodeToString(sum[:16]))
	link := filepath.Join(seedDir, name)
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	n, err := ExportLibFuzzerSeeds(dir, "jsmn", [][]byte{payload})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("written=%d", n)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "secret" {
		t.Fatalf("symlink write escaped: %q err=%v", got, err)
	}
	st, err := os.Lstat(link)
	if err != nil || st.Mode()&os.ModeSymlink != 0 || !st.Mode().IsRegular() {
		t.Fatalf("cache entry mode=%v err=%v", st, err)
	}
	body, err := os.ReadFile(link)
	if err != nil || string(body) != string(payload) {
		t.Fatalf("cache body=%q err=%v", body, err)
	}
}

func TestExportLibFuzzerSeedsRefusesDirSymlink(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(dir, "outside")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, ".cache", "hunt-lf-seeds")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(parent, "jsmn")); err != nil {
		t.Fatal(err)
	}
	if _, err := ExportLibFuzzerSeeds(dir, "jsmn", [][]byte{[]byte("x")}); err == nil {
		t.Fatal("dir symlink must not be a seed cache")
	}
	ents, err := os.ReadDir(outside)
	if err != nil || len(ents) != 0 {
		t.Fatalf("wrote through dir symlink: %v err=%v", ents, err)
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
