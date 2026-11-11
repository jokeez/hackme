package fuzzingcli

import (
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"hackme/internal/fuzzengine"
	"hackme/internal/pathsafe"
)

const (
	digSeedMaxBytes = 65536
	defaultDigSeeds = 256
)

// DigSeedDir is the on-disk import path for external Dig research seeds per guard pack.
func DigSeedDir(repoRoot, packID string) string {
	if repoRoot == "" {
		repoRoot = "."
	}
	absRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		absRoot = repoRoot
	}
	packID = filepath.Base(strings.TrimSpace(packID))
	if packID == "" || packID == "." || packID == ".." {
		packID = "_"
	}
	if p, ok := pathsafe.JoinUnder(absRoot, ".cache", "dig-seeds", packID); ok {
		return p
	}
	// Fail closed: do not return an unsanitized relative path that LoadDigSeedFiles would ignore.
	return ""
}

// LoadDigSeedFiles reads seed inputs from a Dig seed cache directory.
func LoadDigSeedFiles(dir string, maxSeeds int) ([][]byte, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	if maxSeeds <= 0 {
		maxSeeds = defaultDigSeeds
	}
	safeDir, ok := pathsafe.Allow(dir)
	if !ok || !pathsafe.AbsRE.MatchString(safeDir) {
		return nil, nil
	}
	entries, err := os.ReadDir(safeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([][]byte, 0, min(len(entries), maxSeeds))
	seen := map[string]struct{}{}
	for _, ent := range entries {
		if len(out) >= maxSeeds {
			break
		}
		if ent.IsDir() {
			continue
		}
		name, ok := pathsafe.Base(ent.Name())
		if !ok {
			continue
		}
		low := strings.ToLower(name)
		if strings.HasPrefix(low, ".") || strings.HasPrefix(low, "crash-") || low == "readme" {
			continue
		}
		safePath, ok := pathsafe.JoinUnder(safeDir, name)
		if !ok || !pathsafe.AbsRE.MatchString(safePath) {
			continue
		}
		if !pathsafe.AbsRE.MatchString(safePath) {
			continue
		}
		// O_NOFOLLOW: Lstat-then-ReadFile still follows a symlink swapped in
		// between the check and the read (Dig seed cache / host secret exfil).
		b, err := readRegularNoFollow(safePath, digSeedMaxBytes)
		if err != nil || len(b) == 0 {
			continue
		}
		key := hex.EncodeToString(b)
		if len(key) > 64 {
			key = key[:64]
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, b)
	}
	return out, nil
}

// MergeDigSeedCorpus imports cached Dig seeds into campaign config (byte or u64 corpus).
// Returns the number of newly merged seeds.
func MergeDigSeedCorpus(cfg map[string]any, repoRoot, packID string) (int, error) {
	if cfg == nil || strings.TrimSpace(packID) == "" {
		return 0, nil
	}
	seeds, err := LoadDigSeedFiles(DigSeedDir(repoRoot, packID), defaultDigSeeds)
	if err != nil {
		return 0, err
	}
	if len(seeds) == 0 {
		return 0, nil
	}
	if fuzzengine.ParseInputMode(cfg) == fuzzengine.InputModeBytes {
		return mergeDigByteCorpus(cfg, seeds), nil
	}
	return mergeDigU64Corpus(cfg, seeds), nil
}

// ExportDigSeeds writes seed files into the Dig import cache for a pack.
func ExportDigSeeds(repoRoot, packID string, seeds [][]byte) (int, error) {
	dir := DigSeedDir(repoRoot, packID)
	if dir == "" {
		return 0, fmt.Errorf("dig seeds: invalid repo/pack path")
	}
	safe, ok := pathsafe.Allow(dir)
	if !ok {
		return 0, fmt.Errorf("dig seeds: path rejected")
	}
	dir = safe
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	written := 0
	for i, b := range seeds {
		if len(b) == 0 || len(b) > digSeedMaxBytes {
			continue
		}
		name := fmt.Sprintf("seed-%04d-%s.bin", i+1, hex.EncodeToString(b[:min(4, len(b))]))
		path, ok := pathsafe.JoinUnder(dir, name)
		if !ok {
			continue
		}
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func mergeDigByteCorpus(cfg map[string]any, seeds [][]byte) int {
	existing := fuzzengine.ParseByteCorpus(cfg)
	seen := map[string]struct{}{}
	raw := make([]any, 0, len(existing)+len(seeds))
	for _, b := range existing {
		key := hex.EncodeToString(b)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		raw = append(raw, hex.EncodeToString(b))
	}
	added := 0
	for _, b := range seeds {
		key := hex.EncodeToString(b)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		raw = append(raw, key)
		added++
	}
	if added == 0 && len(raw) == len(existing) {
		return 0
	}
	cfg["seed_byte_corpus"] = raw
	return added
}

func mergeDigU64Corpus(cfg map[string]any, seeds [][]byte) int {
	existing := fuzzengine.ParseSeedCorpus(cfg)
	seen := map[uint64]struct{}{}
	raw := make([]any, 0, len(existing)+len(seeds))
	for _, in := range existing {
		if _, ok := seen[in]; ok {
			continue
		}
		seen[in] = struct{}{}
		raw = append(raw, in)
	}
	added := 0
	for _, b := range seeds {
		u := fuzzengine.PackInputBytesToU64(b)
		if _, ok := seen[u]; ok {
			continue
		}
		seen[u] = struct{}{}
		raw = append(raw, u)
		added++
	}
	if added == 0 && len(raw) == len(existing) {
		return 0
	}
	cfg["seed_corpus"] = raw
	return added
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// readRegularNoFollow reads path only when the final component is a regular file.
func readRegularNoFollow(path string, maxSize int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 || (maxSize > 0 && st.Size() > maxSize) {
		return nil, fmt.Errorf("dig corpus: refuse non-regular %s", path)
	}
	return io.ReadAll(io.LimitReader(f, maxSize))
}
