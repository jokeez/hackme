package fuzzingcli

import (
	"os"
	"path/filepath"
	"strings"
)

// PoolSeedFromResearchEnabled reports whether research corpora may feed customer Dig.
// Default OFF — set HACKME_POOL_SEED_FROM_RESEARCH=1 only after proven research hits.
func PoolSeedFromResearchEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_SEED_FROM_RESEARCH")))
	switch v {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// MaybeFeedResearchSeedsToDig copies Hunt/libFuzzer research seeds into Dig seed cache
// when HACKME_POOL_SEED_FROM_RESEARCH is explicitly enabled. Default is a no-op.
func MaybeFeedResearchSeedsToDig(repoRoot, packID, researchTargetID string) (int, error) {
	if !PoolSeedFromResearchEnabled() {
		return 0, nil
	}
	packID = strings.TrimSpace(packID)
	researchTargetID = strings.TrimSpace(researchTargetID)
	if packID == "" || researchTargetID == "" || repoRoot == "" {
		return 0, nil
	}
	dst := DigSeedDir(repoRoot, packID)
	if dst == "" {
		return 0, nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	srcs := []string{
		filepath.Join(repoRoot, ".cache", "hunt-lf-seeds", researchTargetID),
		filepath.Join(repoRoot, "reports", "oss-cve-libfuzzer", researchTargetID, "corpus"),
	}
	n := 0
	for _, src := range srcs {
		ents, err := os.ReadDir(src)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(src, e.Name()))
			if err != nil || len(b) == 0 || len(b) > digSeedMaxBytes {
				continue
			}
			name := "research-" + e.Name()
			if err := os.WriteFile(filepath.Join(dst, name), b, 0o600); err == nil {
				n++
			}
		}
	}
	return n, nil
}
