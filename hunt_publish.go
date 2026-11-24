package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"hackme/internal/hunt"
	"hackme/internal/logsafe"
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
	srcRel := strings.TrimSpace(toString(cfg["hunt_source_rel"]))
	// Catalog / re-place: coordinator GET/HEAD 200 means miners can fetch already.
	// Skip POST upload so we do not burn Orders publish rate (shared with register).
	if ok, remoteSHA := poolsync.CoordinatorHarnessProbe(ctx, coord, token, hash); ok {
		if remoteSHA != "" {
			cfg["harness_content_sha256"] = strings.ToLower(remoteSHA)
		}
		cfg["harness_pool_attached"] = true
		cfg["harness_published"] = true
		cfg["harness_fetch_path"] = hunt.HarnessFetchURL(hash)
		cfg["harness_publish_note"] = "coordinator already has harness — skipped upload"
		return a.syncCoverageHarnessToCoordinator(ctx, coord, token, cfg, srcRel)
	}
	data, err := hunt.GetHarnessArtifact(ctx, a.db, hash)
	if err != nil {
		// Local missing but pool already has bytes (customer published first).
		if poolsync.CoordinatorHarnessAvailable(ctx, coord, token, hash) {
			cfg["harness_pool_attached"] = true
			cfg["harness_publish_note"] = "coordinator already has harness — skipped local upload"
			return a.syncCoverageHarnessToCoordinator(ctx, coord, token, cfg, srcRel)
		}
		// A catalog harness_hash is metadata-derived (id+repo+ref+driver+srcs+flags), so
		// the identical binding rebuilds from upstream_target_id. Without this the campaign
		// is stuck for good once the blob leaves the object store (cache prune, restore on a
		// node that never built it): escrow stays locked, the pool never sees the work and
		// the register is retried forever. Only the create path used to rebuild.
		// Rebuilding is a full clone-and-compile: duktape alone took more than the
		// 300s build wall on the hub while holding the pool-sync worker. Kick it off
		// in the background and let the campaign retry on a later tick, which the
		// per-campaign register backoff makes cheap.
		a.startCatalogHarnessRebuild(cfg, hash)
		return fmt.Errorf("hunt harness sync: local artifact: %w", err)
	}
	if err := poolsync.UploadHuntHarness(ctx, coord, token, hash, data, srcRel); err != nil {
		// Catalog hash is metadata-stable; clang rebuilds can differ byte-for-byte across hosts.
		// Inventory hashes are content-bound — refuse squat on mismatch.
		if poolsync.IsHarnessAlreadyBound(err) {
			ok, remoteSHA := poolsync.CoordinatorHarnessProbe(ctx, coord, token, hash)
			if !ok {
				return fmt.Errorf("hunt harness sync: hash %s already bound but not fetchable", hash)
			}
			if remoteSHA != "" {
				sum := sha256.Sum256(data)
				localSHA := hex.EncodeToString(sum[:])
				if !strings.EqualFold(remoteSHA, localSHA) {
					if huntCatalogSource(cfg) {
						// Pool truth is the first coordinator bind; miners fetch that blob.
						cfg["harness_content_sha256"] = strings.ToLower(remoteSHA)
						cfg["harness_pool_attached"] = true
						cfg["harness_published"] = true
						cfg["harness_fetch_path"] = hunt.HarnessFetchURL(hash)
						cfg["harness_publish_note"] = "catalog metadata-stable hash; kept coordinator bind (local clang rebuild differed)"
						return a.syncCoverageHarnessToCoordinator(ctx, coord, token, cfg, srcRel)
					}
					return fmt.Errorf("hunt harness sync: harness_hash %s already bound to different binary (coordinator %s != local %s)", hash, remoteSHA, localSHA)
				}
			}
		} else {
			return err
		}
	}
	return a.syncCoverageHarnessToCoordinator(ctx, coord, token, cfg, srcRel)
}

// syncCoverageHarnessToCoordinator uploads optional AFL/showmap harness, skipping when
// the coordinator already serves that hash (same publish-rate rationale as primary sync).
func (a *app) syncCoverageHarnessToCoordinator(ctx context.Context, coord, token string, cfg map[string]any, srcRel string) error {
	covHash := strings.TrimSpace(toString(cfg["coverage_harness_hash"]))
	if covHash == "" {
		return nil
	}
	if poolsync.CoordinatorHarnessAvailable(ctx, coord, token, covHash) {
		return nil
	}
	covData, cerr := hunt.GetHarnessArtifact(ctx, a.db, covHash)
	if cerr != nil {
		if covPath := strings.TrimSpace(toString(cfg["coverage_harness_path"])); covPath != "" {
			if perr := hunt.PublishHarnessFile(ctx, a.db, covHash, covPath, srcRel+"#afl"); perr == nil {
				covData, cerr = hunt.GetHarnessArtifact(ctx, a.db, covHash)
			}
		}
	}
	if cerr != nil || len(covData) == 0 {
		return nil
	}
	if uerr := poolsync.UploadHuntHarness(ctx, coord, token, covHash, covData, srcRel+"#afl"); uerr != nil && !poolsync.IsHarnessAlreadyBound(uerr) {
		return fmt.Errorf("hunt coverage harness sync: %w", uerr)
	}
	return nil
}

// rebuildCatalogHarnessArtifact re-materialises a missing catalog harness from its
// pinned upstream target and stores it back under the same metadata-stable hash.
// Catalog-only: inventory hashes are content-bound and must never be re-derived.
func (a *app) rebuildCatalogHarnessArtifact(ctx context.Context, cfg map[string]any, hash string) ([]byte, error) {
	if !huntCatalogSource(cfg) {
		return nil, fmt.Errorf("hunt harness rebuild: not a pinned catalog campaign")
	}
	targetID := strings.TrimSpace(toString(cfg["upstream_target_id"]))
	if targetID == "" {
		return nil, fmt.Errorf("hunt harness rebuild: upstream_target_id missing")
	}
	root := a.repoRoot()
	// EnsureHarnessBinary refuses to return a binary whose catalog hash differs, so a
	// manifest edit cannot silently rebind an existing harness_hash to other sources.
	bin, err := hunt.EnsureHarnessBinary(ctx, root, targetID, hash)
	if err != nil {
		return nil, err
	}
	if err := hunt.PublishHarnessFile(ctx, a.db, hash, bin, strings.TrimSpace(toString(cfg["hunt_source_rel"]))); err != nil {
		if !poolsync.IsHarnessAlreadyBound(err) {
			return nil, err
		}
	}
	return hunt.GetHarnessArtifact(ctx, a.db, hash)
}

// Catalog harness rebuilds run off the sync path: one per hash, at most two at a
// time, so a slow target cannot stall campaign registration or starve the host.
var (
	harnessRebuildMu    sync.Mutex
	harnessRebuildBusy  = map[string]bool{}
	harnessRebuildSlots = make(chan struct{}, 2)
)

func (a *app) startCatalogHarnessRebuild(cfg map[string]any, hash string) {
	hash = strings.TrimSpace(hash)
	if hash == "" || !huntCatalogSource(cfg) {
		return
	}
	// The goroutine must not read the caller's map: copy the three keys it needs.
	cfgCopy := map[string]any{
		"hunt_source":        toString(cfg["hunt_source"]),
		"hunt_source_rel":    toString(cfg["hunt_source_rel"]),
		"upstream_target_id": toString(cfg["upstream_target_id"]),
	}
	harnessRebuildMu.Lock()
	if harnessRebuildBusy[hash] {
		harnessRebuildMu.Unlock()
		return
	}
	harnessRebuildBusy[hash] = true
	harnessRebuildMu.Unlock()

	go func() {
		defer func() {
			harnessRebuildMu.Lock()
			delete(harnessRebuildBusy, hash)
			harnessRebuildMu.Unlock()
		}()
		select {
		case harnessRebuildSlots <- struct{}{}:
			defer func() { <-harnessRebuildSlots }()
		default:
			return // both slots busy; a later tick will try again
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		defer cancel()
		if _, err := a.rebuildCatalogHarnessArtifact(ctx, cfgCopy, hash); err != nil {
			if !isClangRequiredErr(err) {
				log.Printf("hunt harness sync: background rebuild %s failed: %s", logsafe.ID(hash), logsafe.Err(err))
			}
			return
		}
		log.Printf("hunt harness sync: background rebuild %s published", logsafe.ID(hash))
	}()
}
