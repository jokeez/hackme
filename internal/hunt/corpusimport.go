package hunt

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hackme/internal/fuzzengine"
)

const (
	libFuzzerSeedMaxBytes = 65536
	defaultLibFuzzerSeeds = 512
)

// LibFuzzerSeedDir is the on-disk import path for libFuzzer corpus files per catalog target.
func LibFuzzerSeedDir(repoRoot, targetID string) string {
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	return filepath.Join(repoRoot, ".cache", "hunt-lf-seeds", strings.TrimSpace(targetID))
}

// LoadLibFuzzerSeedFiles reads seed inputs from a libFuzzer corpus directory.
func LoadLibFuzzerSeedFiles(dir string, maxSeeds int) ([][]byte, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, nil
	}
	if maxSeeds <= 0 {
		maxSeeds = defaultLibFuzzerSeeds
	}
	entries, err := os.ReadDir(dir)
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
		name := ent.Name()
		low := strings.ToLower(name)
		if strings.HasPrefix(low, ".") || strings.HasPrefix(low, "crash-") || low == "readme" {
			continue
		}
		path := filepath.Join(dir, name)
		st, err := os.Stat(path)
		if err != nil || st.IsDir() || st.Size() <= 0 || st.Size() > libFuzzerSeedMaxBytes {
			continue
		}
		b, err := os.ReadFile(path)
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

// MergeLibFuzzerSeedCorpus imports cached libFuzzer seeds into campaign config seed_byte_corpus.
// Returns the number of newly merged seeds.
func MergeLibFuzzerSeedCorpus(cfg map[string]any, repoRoot, targetID string) (int, error) {
	if cfg == nil || strings.TrimSpace(targetID) == "" {
		return 0, nil
	}
	seeds, err := LoadLibFuzzerSeedFiles(LibFuzzerSeedDir(repoRoot, targetID), defaultLibFuzzerSeeds)
	if err != nil {
		return 0, err
	}
	if len(seeds) == 0 {
		return 0, nil
	}
	merged := mergeSeedByteCorpus(cfg, seeds)
	if merged > 0 {
		ApplyLocalCorpusGuidedDefaults(cfg)
	}
	return merged, nil
}

// ApplyLocalCorpusGuidedDefaults enables L2-style scheduling for node-local Hunt runs.
func ApplyLocalCorpusGuidedDefaults(cfg map[string]any) {
	if cfg == nil {
		return
	}
	cfg["hunt_corpus_guided"] = true
	cfg["guided_scheduling"] = true
	cfg["coverage_guided"] = true
	if _, ok := cfg["corpus_persist"]; !ok {
		cfg["corpus_persist"] = true
	}
}

// ApplyHuntPowerScheduling tunes pool/local mutation depth for Hunt packages.
func ApplyHuntPowerScheduling(cfg map[string]any, pkgKey string) {
	if cfg == nil {
		return
	}
	pkgKey = strings.TrimSpace(strings.ToLower(pkgKey))
	minCap := 0
	switch pkgKey {
	case "hunt_standard", "standard":
		minCap = 10
	case "hunt_heavy", "heavy":
		minCap = 12
	case "hunt_lite", "lite":
		minCap = 6
	}
	if minCap > 0 {
		cur := int(cfgInt(cfg, "power_mut_cap"))
		if cur < minCap {
			cfg["power_mut_cap"] = minCap
		}
	}
}

func mergeSeedByteCorpus(cfg map[string]any, seeds [][]byte) int {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ExportLibFuzzerSeeds writes seed files into the libFuzzer import cache for a target.
func ExportLibFuzzerSeeds(repoRoot, targetID string, seeds [][]byte) (int, error) {
	dir := LibFuzzerSeedDir(repoRoot, targetID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	written := 0
	for i, b := range seeds {
		if len(b) == 0 || len(b) > libFuzzerSeedMaxBytes {
			continue
		}
		name := fmt.Sprintf("seed-%04d-%s.bin", i+1, hex.EncodeToString(b[:min(4, len(b))]))
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, b, 0o644); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}
