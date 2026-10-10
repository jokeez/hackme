package fuzzingcli

import (
	"os"
	"path/filepath"
	"strings"

	"hackme/internal/pathsafe"
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
	tid, ok := pathsafe.Base(researchTargetID)
	if !ok {
		return 0, nil
	}
	absRoot, ok := pathsafe.Allow(repoRoot)
	if !ok {
		// Relative test roots: Abs via Allow after Abs.
		if abs, err := filepath.Abs(repoRoot); err == nil {
			absRoot, ok = pathsafe.Allow(abs)
		}
		if !ok {
			return 0, nil
		}
	}
	dst := DigSeedDir(absRoot, packID)
	if dst == "" {
		return 0, nil
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return 0, err
	}
	srcCandidates := [][]string{
		{".cache", "hunt-lf-seeds", tid},
		{"reports", "oss-cve-libfuzzer", tid, "corpus"},
	}
	n := 0
	for _, parts := range srcCandidates {
		src, ok := pathsafe.JoinUnder(absRoot, parts...)
		if !ok {
			continue
		}
		ents, err := os.ReadDir(src)
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			base, ok := pathsafe.Base(e.Name())
			if !ok || strings.HasPrefix(base, ".") {
				continue
			}
			srcPath, ok := pathsafe.JoinUnder(src, base)
			if !ok {
				continue
			}
			b, err := os.ReadFile(srcPath)
			if err != nil || len(b) == 0 || len(b) > digSeedMaxBytes {
				continue
			}
			outName, ok := pathsafe.Base("research-" + base)
			if !ok {
				continue
			}
			dstPath, ok := pathsafe.JoinUnder(dst, outName)
			if !ok {
				continue
			}
			if err := os.WriteFile(dstPath, b, 0o600); err == nil {
				n++
			}
		}
	}
	return n, nil
}
