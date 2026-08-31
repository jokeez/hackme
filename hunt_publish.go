package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hackme/internal/hunt"
	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
)

func (a *app) publishHuntHarnessForConfig(ctx context.Context, cfg map[string]any) error {
	if cfg == nil || !poolfuzz.IsHuntCampaign(cfg) {
		return nil
	}
	hash := strings.TrimSpace(toString(cfg["harness_hash"]))
	if hash == "" {
		return nil
	}
	root := a.repoRoot()
	cachePath := filepath.Join(root, ".cache", "hunt-harness", hash+".bin")
	if _, err := os.Stat(cachePath); err != nil {
		targetID := strings.TrimSpace(toString(cfg["upstream_target_id"]))
		if targetID == "" {
			return fmt.Errorf("hunt publish: harness binary missing for %s", hash)
		}
		bin, err := hunt.EnsureHarnessBinary(ctx, root, targetID, hash)
		if err != nil {
			return err
		}
		cachePath = bin
	}
	sourceRel := strings.TrimSpace(toString(cfg["hunt_source_rel"]))
	if err := hunt.PublishHarnessFile(ctx, a.db, hash, cachePath, sourceRel); err != nil {
		return err
	}
	cfg["harness_published"] = true
	cfg["harness_fetch_path"] = hunt.HarnessFetchURL(hash)
	return nil
}

func (a *app) syncHuntHarnessToCoordinator(ctx context.Context, cfg map[string]any) {
	if cfg == nil {
		return
	}
	hash := strings.TrimSpace(toString(cfg["harness_hash"]))
	if hash == "" {
		return
	}
	coord := poolsync.ResolveCoordinatorURL()
	token := poolsync.AdminToken()
	if coord == "" || token == "" {
		return
	}
	data, err := hunt.GetHarnessArtifact(ctx, a.db, hash)
	if err != nil {
		return
	}
	_ = poolsync.UploadHuntHarness(ctx, coord, token, hash, data, strings.TrimSpace(toString(cfg["hunt_source_rel"])))
}
