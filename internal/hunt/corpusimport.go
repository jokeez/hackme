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
	// rankedLibFuzzerSeedCap keeps L2 imports lean — dump-all seeds can hurt first-hit.
	rankedLibFuzzerSeedCap = 64
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
	root := RepoRoot()
	if root == "" {
		root = filepath.Dir(dir)
	}
	entries, err := SafeReadDirUnder(root, dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		// Fall back: allowlisted absolute dir outside repo root (tests/tmp).
		safeDir, aerr := allowlistedAbs(dir)
		if aerr != nil {
			return nil, err
		}
		entries, err = os.ReadDir(safeDir)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil
			}
			return nil, err
		}
		dir = safeDir
		root = filepath.Dir(safeDir)
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
		abs, jerr := SafeJoinUnder(dir, name)
		if jerr != nil {
			continue
		}
		st, serr := SafeStatUnder(root, abs)
		if serr != nil || st.IsDir() || st.Size() <= 0 || st.Size() > libFuzzerSeedMaxBytes {
			continue
		}
		b, rerr := SafeReadFileUnder(root, abs)
		if rerr != nil || len(b) == 0 {
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
// Seeds are rarity-ranked and capped (not dump-all) so Hunt shards start with a lean L2 set.
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
	seeds = RankLibFuzzerSeeds(seeds, rankedLibFuzzerSeedCap)
	merged := mergeSeedByteCorpus(cfg, seeds)
	if merged > 0 {
		ApplyLocalCorpusGuidedDefaults(cfg)
	}
	return merged, nil
}

// RankLibFuzzerSeeds orders LF corpus by structural rarity / compactness and keeps at most cap.
func RankLibFuzzerSeeds(seeds [][]byte, capN int) [][]byte {
	if len(seeds) == 0 {
		return nil
	}
	if capN <= 0 {
		capN = rankedLibFuzzerSeedCap
	}
	pool := make([]fuzzengine.PoolCorpusSeed, 0, len(seeds))
	for _, b := range seeds {
		if len(b) == 0 {
			continue
		}
		edge, path := fuzzengine.CoverageBucketsStructural(b)
		pool = append(pool, fuzzengine.PoolCorpusSeed{
			InputBytes: append([]byte(nil), b...),
			Energy:     2,
			Edge:       edge,
			Path:       path,
		})
	}
	if len(pool) == 0 {
		return nil
	}
	rarity := fuzzengine.BuildEdgeHitCounts(pool)
	order := fuzzengine.RankCorpusForCull(pool, rarity)
	if len(order) > capN {
		order = order[:capN]
	}
	out := make([][]byte, 0, len(order))
	for _, i := range order {
		out = append(out, pool[i].InputBytes)
	}
	return out
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
	fuzzengine.EnableDeepHavocV28(cfg)
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
		minCap = 14 // was 10 — deeper power stages with v2.8
	case "hunt_heavy", "heavy":
		// Heavy opts into higher power_mut_cap/stack (16) unless explicitly disabled.
		minCap = 16
		if v, ok := cfg["hunt_heavy_power_boost"]; ok {
			switch t := v.(type) {
			case bool:
				if !t {
					minCap = 12
				}
			case string:
				s := strings.TrimSpace(strings.ToLower(t))
				if s == "0" || s == "false" || s == "off" || s == "no" {
					minCap = 12
				}
			}
		}
	case "hunt_lite", "lite":
		minCap = 8 // was 6
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
