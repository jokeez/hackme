package hunt

import (
	"context"
	"fmt"
	"os"
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
	if v, ok := harnessCache.Load(hash); ok {
		if p, ok := v.(string); ok && p != "" {
			return p, nil
		}
	}
	cachePath := huntHarnessCachePath(repoRoot, hash)
	if st, err := osStat(cachePath); err == nil && st {
		harnessCache.Store(hash, cachePath)
		return cachePath, nil
	}
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
	return res.BinaryPath, nil
}

func huntHarnessCachePath(repoRoot, hash string) string {
	return strings.TrimRight(repoRoot, "/") + "/.cache/hunt-harness/" + hash + ".bin"
}

func osStat(path string) (bool, error) {
	st, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	return st.Mode().IsRegular(), nil
}
