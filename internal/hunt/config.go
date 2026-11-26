package hunt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzescrow"
	"hackme/internal/fuzzupstream"
)

const defaultHuntIterationsPerShard = huntIterPerShardLite

// CampaignConfig builds normalized fuzz campaign config for Hunt.
func CampaignConfig(ctx context.Context, repoRoot string, req CreateRequest) (map[string]any, string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	pkgKey := strings.TrimSpace(strings.ToLower(req.Package))
	if pkgKey == "" {
		pkgKey = "hunt_lite"
	}
	preset := PackageByKey(pkgKey)
	if preset == nil {
		return nil, "", fmt.Errorf("hunt: unknown package %q", req.Package)
	}
	budgetHMC := req.BudgetHMC
	if budgetHMC <= 0 {
		budgetHMC = preset.BudgetHMC
	}
	minBudget := fuzzescrow.MinBudgetHMCForHuntPackage(pkgKey)
	if budgetHMC < minBudget {
		return nil, "", fmt.Errorf("hunt: budget_hmc below minimum %.0f for %s", minBudget, pkgKey)
	}

	var pin *RepoPinResult
	var invRoot string
	// An already-published harness is identified by hash. Joining the
	// customer's relative path onto the hub install root (/opt/hackme) and
	// Lstat'ing it fails closed with invalid_hunt_config before the
	// coordinator probe can run. A local Repo pin still resolves, so a
	// desktop create keeps the source-hash check.
	publishedAttach := req.PoolDistributed && strings.TrimSpace(req.HarnessHash) != ""
	if req.Repo != nil && (strings.TrimSpace(req.Repo.Path) != "" || strings.TrimSpace(req.Repo.GitURL) != "") {
		p, err := PinRepo(ctx, repoRoot, *req.Repo)
		if err != nil {
			return nil, "", err
		}
		pin = p
		invRoot = p.Path
	} else if !publishedAttach && req.Inventory != nil && strings.TrimSpace(req.Inventory.Path) != "" {
		abs, err := resolveInventoryRoot(repoRoot, invRootFromInventory(req.Inventory))
		if err != nil {
			return nil, "", err
		}
		invRoot = abs
	}

	targetID, targetTitle, sourceRel, err := resolveTarget(repoRoot, req, invRoot)
	if err != nil {
		return nil, "", err
	}

	pool := req.PoolDistributed
	huntSource := "catalog"
	cfg := map[string]any{
		"depth_tier":             string(fuzzengine.DepthOSSCVE),
		"input_mode":             "bytes",
		"native_repro_mode":      "oss_upstream",
		"escrow_split":           fuzzescrow.EscrowSplit5050,
		"hunt_package":           preset.Key,
		"upstream_target":        "oss",
		"upstream_target_id":     targetID,
		"hunt_source":            huntSource,
		"pool_distributed":       pool,
		"auto_runner":            "1",
		"bounty_requires_native": true,
		"hunt_local_runner":      !pool,
	}

	if pin != nil {
		cfg["hunt_pin_path"] = pin.Path
		cfg["hunt_pin_sha"] = pin.CommitSHA
		if pin.GitURL != "" {
			cfg["hunt_git_url"] = pin.GitURL
			cfg["hunt_git_ref"] = pin.Ref
		}
	}
	if invRoot != "" {
		cfg["hunt_inventory_root"] = invRoot
	}

	if strings.HasPrefix(targetID, "inv_") || (req.Inventory != nil && !req.Catalog) {
		huntSource = "inventory"
		cfg["hunt_source"] = huntSource
		cfg["upstream_target"] = "inventory"
		if sourceRel == "" && req.Inventory != nil {
			sourceRel = strings.TrimSpace(req.Inventory.Path)
		}
		if sourceRel == "" {
			return nil, "", errors.New("hunt: inventory source_rel required")
		}
		cfg["hunt_source_rel"] = sourceRel
		if pin == nil && invRoot != "" {
			cfg["hunt_pin_path"] = invRoot
		}
		buildPin := pin
		if buildPin == nil && invRoot != "" {
			buildPin = &RepoPinResult{Path: invRoot, PinnedAt: time.Now().Unix()}
		}
		wantHash := strings.TrimSpace(strings.ToLower(req.HarnessHash))
		if pool && wantHash != "" {
			// Windows / no-bwrap Orders: attach campaign to already-published pool harness.
			// Without a local pin there is nothing to hash on disk — the coordinator
			// content sha below is what binds the campaign to the uploaded binary.
			var hash string
			if buildPin != nil || strings.TrimSpace(invRoot) != "" {
				var aErr error
				hash, _, aErr = AttachPublishedInventoryHarness(repoRoot, buildPin, invRoot, sourceRel, wantHash)
				if aErr != nil {
					return nil, "", aErr
				}
			} else if !ValidHarnessHash(wantHash) {
				return nil, "", fmt.Errorf("hunt: invalid harness_hash %q", wantHash)
			} else {
				hash = wantHash
			}
			ok, contentSHA := ProbeCoordinatorHarness(ctx, hash)
			if !ok {
				return nil, "", fmt.Errorf("hunt: harness_hash %s not fetchable from coordinator — publish via POST /api/fuzz/pool/hunt/harness (or /api/hunt/harness/publish) first", hash)
			}
			wantContent := strings.ToLower(strings.TrimSpace(req.HarnessContentSHA256))
			gotContent := strings.ToLower(strings.TrimSpace(contentSHA))
			if wantContent == "" {
				return nil, "", fmt.Errorf("hunt: harness_content_sha256 required for pool attach (use content_sha256 from harness publish response — blocks inventory-hash squat)")
			}
			if !ValidContentSHA256(wantContent) {
				return nil, "", fmt.Errorf("hunt: invalid harness_content_sha256")
			}
			if gotContent == "" {
				return nil, "", fmt.Errorf("hunt: coordinator harness probe missing content sha256 for %s", hash)
			}
			if wantContent != gotContent {
				return nil, "", fmt.Errorf("hunt: harness_content_sha256 mismatch (want %s from builder, got %s from coordinator — refuse squat/wrong binary)", wantContent, gotContent)
			}
			cfg["harness_hash"] = hash
			cfg["harness_fetch_path"] = HarnessFetchURL(hash)
			cfg["harness_pool_attached"] = true
			cfg["harness_published"] = true
			cfg["harness_build_deferred"] = false
			cfg["harness_publish_note"] = "attached published pool harness — local ASAN/bwrap build skipped"
			cfg["harness_content_sha256"] = gotContent
			// No local afl-cc on Windows attach path — honest floor until coverage harness published.
			cfg["showmap_instrumented"] = false
			cfg["hunt_edge_pay"] = "floor_only"
			cfg["showmap_instrument_note"] = "pool-attached harness — edge PayRun floor_only until afl-cc coverage harness is published (ASAN crashes still pay)"
		} else {
			build, bErr := BuildInventoryHarness(ctx, repoRoot, HarnessBuildRequest{
				Pin:            buildPin,
				SourceRel:      sourceRel,
				TemplateAccept: req.TemplateAccept,
			})
			if bErr != nil {
				return nil, "", bErr
			}
			cfg["harness_hash"] = build.HarnessHash
			if pool {
				cfg["harness_fetch_path"] = HarnessFetchURL(build.HarnessHash)
			}
			// Best-effort afl-cc showmap binary for coordinator prove (no pin on hub).
			if aflBin, aflErr := BuildInventoryAFLHarness(ctx, repoRoot, HarnessBuildRequest{
				Pin:            buildPin,
				SourceRel:      sourceRel,
				TemplateAccept: req.TemplateAccept,
			}); aflErr == nil && aflBin != "" {
				covHash := CoverageHarnessHash(build.HarnessHash)
				cfg["coverage_harness_hash"] = covHash
				cfg["coverage_harness_path"] = aflBin
				cfg["showmap_instrumented"] = true
				cfg["hunt_edge_pay"] = "showmap"
				if pool {
					cfg["coverage_harness_fetch_path"] = HarnessFetchURL(covHash)
				}
			} else {
				// Honest customer signal: ASAN pay still works; edge PayRun needs afl-cc.
				cfg["showmap_instrumented"] = false
				cfg["hunt_edge_pay"] = "floor_only"
				if aflErr != nil {
					cfg["showmap_instrument_note"] = "afl-cc inventory harness unavailable — CLEAN edge pay is floor until coverage harness is published (ASAN crashes still pay)"
				} else {
					cfg["showmap_instrument_note"] = "no afl-cc coverage harness — edge PayRun floor_only (ASAN crashes still pay)"
				}
			}
		}
	}

	if pool {
		cfg["auto_runner"] = "0"
		cfg["work_kind"] = "hunt_shard"
		cfg["check_semantics"] = "native_crash"
		cfg["hunt_segment_mutating"] = true
		cfg["power_mut_cap"] = fuzzengine.DefaultPowerMutCap(fuzzengine.DepthOSSCVE)
		ApplyPoolGuidedDefaults(cfg, targetID)
		ApplyHuntPowerScheduling(cfg, preset.Key)
		if _, ok := cfg["harness_hash"]; !ok {
			hash, hErr := CatalogHarnessHash(repoRoot, targetID)
			if hErr != nil {
				return nil, "", hErr
			}
			cfg["harness_hash"] = hash
		}
	}
	ApplySanitizerDefaults(cfg, pkgKey)
	ApplyPackageDepthDefaults(cfg, pkgKey, pool)
	ApplyCoverageEngineDefaults(cfg, pkgKey)
	ApplyHuntMutatorDict(cfg, targetID)
	// Explicit create overrides (bootstrap / CLI) win over package defaults.
	if eng := strings.TrimSpace(req.HuntEngine); eng != "" {
		cfg["hunt_engine"] = ParseHuntEngine(eng)
	}
	if req.EngineLeaseSec > 0 {
		cfg["engine_lease_sec"] = clampEngineLeaseSec(req.EngineLeaseSec)
	}
	// Inventory public packs: corpus/*.bin → L2/seed_byte_corpus; fuzz/*.dict → libfuzzer_dict.
	if huntSource == "inventory" {
		pinForPack := strings.TrimSpace(toString(cfg["hunt_pin_path"]))
		if pinForPack == "" {
			pinForPack = invRoot
		}
		if err := ApplyInventoryPackAssets(cfg, repoRoot, targetID, pinForPack, sourceRel); err != nil {
			return nil, "", err
		}
	} else if _, err := MergeLibFuzzerSeedCorpus(cfg, repoRoot, targetID); err != nil {
		// Optional on desktop Orders (empty cache / Windows path allowlist). Pool workers carry seeds.
		cfg["libfuzzer_seed_note"] = err.Error()
	}
	// Pool engine walls: long enough for LF/AFL to warm corpus, short enough to reclaim.
	// (Previously Standard/Heavy were capped at 90s — too shallow vs local overnight.)
	if pool {
		if _, ok := cfg["engine_lease_sec"]; !ok {
			cfg["engine_lease_sec"] = PoolEngineLeaseSecForPackage(pkgKey)
		}
		// Shared L2 corpus across miners. Lite was stuck at the 384 guided default.
		switch pkgKey {
		case "hunt_heavy", "heavy":
			if n := intFromAny(cfg["pool_corpus_max"]); n < 768 {
				cfg["pool_corpus_max"] = 768
			}
		default:
			if n := intFromAny(cfg["pool_corpus_max"]); n < 512 {
				cfg["pool_corpus_max"] = 512
			}
		}
	}
	// Catalog targets have no pack *.dict; stamp a profile libFuzzer dict after
	// inventory packs so a published fuzz/*.dict still wins.
	EnsureLibFuzzerDict(cfg, targetID)

	if req.Inventory != nil && strings.TrimSpace(req.Inventory.Path) != "" {
		cfg["hunt_inventory_path"] = strings.TrimSpace(req.Inventory.Path)
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = "Hunt · " + targetTitle
	}
	return cfg, title, nil
}

func invRootFromInventory(inv *TargetSummary) string {
	if inv == nil {
		return ""
	}
	return strings.TrimSpace(inv.Path)
}

func resolveTarget(repoRoot string, req CreateRequest, invRoot string) (id, title, sourceRel string, err error) {
	if req.Inventory != nil && strings.TrimSpace(req.Inventory.Path) != "" {
		inv := req.Inventory
		return inventoryTargetID(inv.Path), inv.Title, strings.TrimSpace(inv.Path), nil
	}
	targetID := strings.TrimSpace(req.TargetID)
	if targetID == "" {
		return "", "", "", errors.New("hunt: target_id or inventory_target required")
	}
	if req.Catalog || !strings.HasPrefix(targetID, "inv_") {
		manifest, mErr := fuzzupstream.LoadManifest(repoRoot)
		if mErr != nil {
			return "", "", "", mErr
		}
		t, err := manifest.TargetByID(targetID)
		if err != nil {
			return "", "", "", err
		}
		if strings.TrimSpace(t.Driver) == "" {
			return "", "", "", fmt.Errorf("hunt: target %q has no fuzz driver (reuse not ready)", targetID)
		}
		return t.ID, t.Title, "", nil
	}
	if invRoot != "" && strings.HasPrefix(targetID, "inv_") {
		// resolve relative path from scan
		res, sErr := ScanInventory(repoRoot, invRoot, 0, 0)
		if sErr == nil {
			for _, t := range res.Targets {
				if t.ID == targetID {
					return t.ID, t.Title, t.Path, nil
				}
			}
		}
	}
	return targetID, targetID, "", nil
}

// BudgetForCreate returns escrow budget and shard count after package defaults.
func BudgetForCreate(req CreateRequest) (budgetHMC float64, shards int, pkgKey string, err error) {
	pkgKey = strings.TrimSpace(strings.ToLower(req.Package))
	if pkgKey == "" {
		pkgKey = "hunt_lite"
	}
	preset := PackageByKey(pkgKey)
	if preset == nil {
		return 0, 0, "", fmt.Errorf("hunt: unknown package %q", req.Package)
	}
	budgetHMC = req.BudgetHMC
	if budgetHMC <= 0 {
		budgetHMC = preset.BudgetHMC
	}
	shards = req.BudgetShards
	if shards <= 0 {
		shards = preset.BudgetShards
	}
	// Clamp to escrow floor so tiny smoke shards don't fail OpenHuntEscrow.
	if shards < fuzzescrow.HuntMinShards {
		shards = fuzzescrow.HuntMinShards
	}
	return budgetHMC, shards, pkgKey, nil
}
