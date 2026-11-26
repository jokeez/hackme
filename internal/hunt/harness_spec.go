package hunt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// HarnessSpec selects catalog OSS or customer inventory harness.
type HarnessSpec struct {
	Source      string // catalog | inventory
	TargetID    string
	HarnessHash string
	PinPath     string
	SourceRel   string
}

// HarnessSpecFromConfig builds a spec from campaign config map.
func HarnessSpecFromConfig(cfg map[string]any) HarnessSpec {
	if cfg == nil {
		return HarnessSpec{}
	}
	src := strings.TrimSpace(toString(cfg["hunt_source"]))
	if src == "" {
		src = "catalog"
	}
	return HarnessSpec{
		Source:      src,
		TargetID:    strings.TrimSpace(toString(cfg["upstream_target_id"])),
		HarnessHash: strings.TrimSpace(toString(cfg["harness_hash"])),
		PinPath:     strings.TrimSpace(toString(cfg["hunt_pin_path"])),
		SourceRel:   strings.TrimSpace(toString(cfg["hunt_source_rel"])),
	}
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch x := v.(type) {
	case string:
		return x
	default:
		return fmt.Sprint(v)
	}
}

// EnsureHarness resolves and returns harness binary path for catalog or inventory.
func EnsureHarness(ctx context.Context, repoRoot string, spec HarnessSpec) (string, error) {
	if strings.EqualFold(spec.Source, "inventory") {
		return ensureInventoryHarness(ctx, repoRoot, spec)
	}
	return EnsureHarnessBinary(ctx, repoRoot, spec.TargetID, spec.HarnessHash)
}

func ensureInventoryHarness(ctx context.Context, repoRoot string, spec HarnessSpec) (string, error) {
	hash := strings.TrimSpace(spec.HarnessHash)
	if hash == "" {
		return "", fmt.Errorf("hunt: inventory harness_hash required")
	}
	if repoRoot == "" {
		repoRoot = RepoRoot()
	}
	cachePath := huntHarnessCachePath(repoRoot, hash)
	if _, _, err := readVerifiedHarnessCache(cachePath, ""); err == nil {
		harnessCache.Store(hash, cachePath)
		return cachePath, nil
	}
	harnessCache.Delete(hash)
	quarantineHarnessCache(cachePath)
	if spec.PinPath == "" || spec.SourceRel == "" {
		return "", fmt.Errorf("hunt: inventory harness %s not built on this node", hash)
	}
	pin := &RepoPinResult{Path: spec.PinPath, CommitSHA: ""}
	res, err := BuildInventoryHarness(ctx, repoRoot, HarnessBuildRequest{
		Pin:            pin,
		SourceRel:      spec.SourceRel,
		TemplateAccept: true,
	})
	if err != nil {
		return "", err
	}
	binPath := strings.TrimSpace(res.BinaryPath)
	if binPath == "" {
		return "", fmt.Errorf("hunt: inventory build returned empty path")
	}
	data, err := os.ReadFile(binPath)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		return "", err
	}
	tmp := cachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, cachePath); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := writeHarnessCacheAttestation(cachePath, contentSHA256Hex(data)); err != nil {
		quarantineHarnessCache(cachePath)
		return "", err
	}
	harnessCache.Store(hash, cachePath)
	return cachePath, nil
}

func huntHarnessCachePath(repoRoot, hash string) string {
	hash = strings.TrimSpace(hash)
	if !ValidHarnessHash(hash) {
		hash = "invalid"
	}
	return filepath.Join(strings.TrimRight(repoRoot, "/"), ".cache", "hunt-harness", hash+".bin")
}

func osStat(path string) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return st.Mode().IsRegular(), nil
}
