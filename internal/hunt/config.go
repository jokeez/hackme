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

const defaultHuntIterationsPerShard = 32

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
	shards := req.BudgetShards
	if shards <= 0 {
		shards = preset.BudgetShards
	}

	var pin *RepoPinResult
	var invRoot string
	if req.Repo != nil && (strings.TrimSpace(req.Repo.Path) != "" || strings.TrimSpace(req.Repo.GitURL) != "") {
		p, err := PinRepo(ctx, repoRoot, *req.Repo)
		if err != nil {
			return nil, "", err
		}
		pin = p
		invRoot = p.Path
	} else if req.Inventory != nil && strings.TrimSpace(req.Inventory.Path) != "" {
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
			cfg["hunt_pool_note"] = "inventory pool requires workers with shared hunt-harness cache"
		}
	}

	if pool {
		cfg["auto_runner"] = "0"
		cfg["work_kind"] = "hunt_shard"
		cfg["check_semantics"] = "native_crash"
		cfg["iterations_per_shard"] = defaultHuntIterationsPerShard
		if _, ok := cfg["harness_hash"]; !ok {
			hash, hErr := CatalogHarnessHash(repoRoot, targetID)
			if hErr != nil {
				return nil, "", hErr
			}
			cfg["harness_hash"] = hash
		}
	}

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
	return budgetHMC, shards, pkgKey, nil
}
