package main

import (
	"context"
	"fmt"
	"strings"

	"hackme/internal/hunt"
	"hackme/internal/poolfuzz"
	"hackme/internal/poolsync"
)

func huntCatalogSource(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	src := strings.ToLower(strings.TrimSpace(toString(cfg["hunt_source"])))
	if src == "catalog" || src == "" {
		// Inventory always sets hunt_source_rel; catalog pool uses upstream_target_id only.
		if strings.TrimSpace(toString(cfg["hunt_source_rel"])) != "" {
			return false
		}
		return strings.TrimSpace(toString(cfg["upstream_target_id"])) != ""
	}
	return false
}

func isClangRequiredErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "clang required")
}

func (a *app) publishHuntHarnessForConfig(ctx context.Context, cfg map[string]any) error {
	if cfg == nil || !poolfuzz.IsHuntCampaign(cfg) {
		return nil
	}
	hash := strings.TrimSpace(toString(cfg["harness_hash"]))
	if hash == "" {
		return nil
	}
	// Already attached to a pool-published inventory harness (Windows OOTB) — no local blob.
	if attached, _ := cfg["harness_pool_attached"].(bool); attached {
		cfg["harness_published"] = true
		cfg["harness_fetch_path"] = hunt.HarnessFetchURL(hash)
		if strings.TrimSpace(toString(cfg["harness_publish_note"])) == "" {
			cfg["harness_publish_note"] = "attached published pool harness — local ASAN/bwrap build skipped"
		}
		return nil
	}
	root := a.repoRoot()
	cachePath, err := hunt.SafeCacheFile(root, "hunt-harness", hash, "bin")
	if err != nil {
		return err
	}
	if _, err := hunt.SafeStatUnder(root, cachePath); err != nil {
		targetID := strings.TrimSpace(toString(cfg["upstream_target_id"]))
		if targetID == "" {
			return fmt.Errorf("hunt publish: harness binary missing for %s", hash)
		}
		bin, err := hunt.EnsureHarnessBinary(ctx, root, targetID, hash)
		if err != nil {
			// Desktop Orders (esp. Windows) often lack clang — catalog pool campaigns
			// still open escrow; fleet miners rebuild from upstream_target_id.
			if huntCatalogSource(cfg) && isClangRequiredErr(err) {
				cfg["harness_build_deferred"] = true
				cfg["harness_fetch_path"] = hunt.HarnessFetchURL(hash)
				cfg["harness_publish_note"] = "local clang missing — harness build deferred to pool miners"
				return nil
			}
			return err
		}
		cachePath = bin
	}
	sourceRel := strings.TrimSpace(toString(cfg["hunt_source_rel"]))
	if err := hunt.PublishHarnessFile(ctx, a.db, hash, cachePath, sourceRel); err != nil {
		// Catalog harness_hash is metadata-stable across clang rebuilds. A newer
		// local .bin can differ byte-for-byte from the first bind and must not
		// block campaign create (same tolerance as coordinator upload).
		if huntCatalogSource(cfg) && poolsync.IsHarnessAlreadyBound(err) {
			if _, gerr := hunt.GetHarnessArtifact(ctx, a.db, hash); gerr == nil {
				cfg["harness_published"] = true
				cfg["harness_fetch_path"] = hunt.HarnessFetchURL(hash)
				cfg["harness_publish_note"] = "local rebuild differed; kept existing harness bind"
				return nil
			}
		}
		return err
	}
	cfg["harness_published"] = true
	cfg["harness_fetch_path"] = hunt.HarnessFetchURL(hash)
	// Publish afl-cc showmap harness when built at campaign create.
	if covHash := strings.TrimSpace(toString(cfg["coverage_harness_hash"])); covHash != "" {
		covPath := strings.TrimSpace(toString(cfg["coverage_harness_path"]))
		if covPath != "" {
			if err := hunt.PublishHarnessFile(ctx, a.db, covHash, covPath, sourceRel+"#afl"); err != nil {
				return fmt.Errorf("hunt publish coverage harness: %w", err)
			}
			cfg["coverage_harness_fetch_path"] = hunt.HarnessFetchURL(covHash)
		}
	}
	return nil
}

func (a *app) syncHuntHarnessToCoordinator(ctx context.Context, cfg map[string]any) error {
	if cfg == nil {
		return nil
	}
	hash := strings.TrimSpace(toString(cfg["harness_hash"]))
	if hash == "" {
		return nil
	}
	coord := poolsync.ResolveCoordinatorURL()
	token := poolsync.AdminToken()
	if coord == "" || token == "" {
		return fmt.Errorf("hunt harness sync: coordinator url/token not configured")
	}
	// Pool-attached: blob already on coordinator — skip local read/upload.
	if attached, _ := cfg["harness_pool_attached"].(bool); attached {
		if poolsync.CoordinatorHarnessAvailable(ctx, coord, token, hash) {
			return nil
		}
		return fmt.Errorf("hunt harness sync: attached hash %s missing on coordinator", hash)
	}
	data, err := hunt.GetHarnessArtifact(ctx, a.db, hash)
	if err != nil {
		// Local missing but pool already has bytes (customer published first).
		if poolsync.CoordinatorHarnessAvailable(ctx, coord, token, hash) {
			cfg["harness_pool_attached"] = true
			cfg["harness_publish_note"] = "coordinator already has harness — skipped local upload"
			return nil
		}
		return fmt.Errorf("hunt harness sync: local artifact: %w", err)
	}
	srcRel := strings.TrimSpace(toString(cfg["hunt_source_rel"]))
	if err := poolsync.UploadHuntHarness(ctx, coord, token, hash, data, srcRel); err != nil {
		// Catalog hash is metadata-stable; clang rebuilds can differ byte-for-byte across hosts.
		// "already bound" from the coordinator means that hash is already published and fetchable.
		if poolsync.IsHarnessAlreadyBound(err) {
			// continue to coverage harness upload
		} else {
			return err
		}
	}
	if covHash := strings.TrimSpace(toString(cfg["coverage_harness_hash"])); covHash != "" {
		covData, cerr := hunt.GetHarnessArtifact(ctx, a.db, covHash)
		if cerr != nil {
			// Publish from on-disk path if not yet in local artifact DB.
			if covPath := strings.TrimSpace(toString(cfg["coverage_harness_path"])); covPath != "" {
				if perr := hunt.PublishHarnessFile(ctx, a.db, covHash, covPath, srcRel+"#afl"); perr == nil {
					covData, cerr = hunt.GetHarnessArtifact(ctx, a.db, covHash)
				}
			}
		}
		if cerr == nil && len(covData) > 0 {
			if uerr := poolsync.UploadHuntHarness(ctx, coord, token, covHash, covData, srcRel+"#afl"); uerr != nil && !poolsync.IsHarnessAlreadyBound(uerr) {
				return fmt.Errorf("hunt coverage harness sync: %w", uerr)
			}
		}
	}
	return nil
}
