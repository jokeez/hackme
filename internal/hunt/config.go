package hunt

import (
	"errors"
	"fmt"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzescrow"
	"hackme/internal/fuzzupstream"
)

// CampaignConfig builds normalized fuzz campaign config for Hunt.
func CampaignConfig(repoRoot string, req CreateRequest) (map[string]any, string, error) {
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

	targetID, targetTitle, err := resolveTarget(repoRoot, req)
	if err != nil {
		return nil, "", err
	}

	cfg := map[string]any{
		"depth_tier":       string(fuzzengine.DepthOSSCVE),
		"input_mode":       "bytes",
		"native_repro_mode": "asan_binary",
		"escrow_split":     fuzzescrow.EscrowSplit5050,
		"hunt_package":     preset.Key,
		"upstream_target":  "oss",
		"upstream_target_id": targetID,
		"pool_distributed": false,
		"auto_runner":      "1",
		"bounty_requires_native": true,
		"hunt_local_runner": true,
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

func resolveTarget(repoRoot string, req CreateRequest) (id, title string, err error) {
	if req.Inventory != nil && strings.TrimSpace(req.Inventory.Path) != "" {
		inv := req.Inventory
		return inventoryTargetID(inv.Path), inv.Title, nil
	}
	targetID := strings.TrimSpace(req.TargetID)
	if targetID == "" {
		return "", "", errors.New("hunt: target_id or inventory_target required")
	}
	if req.Catalog || strings.HasPrefix(targetID, "inv_") == false {
		manifest, mErr := fuzzupstream.LoadManifest(repoRoot)
		if mErr != nil {
			return "", "", mErr
		}
		t, err := manifest.TargetByID(targetID)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(t.Driver) == "" {
			return "", "", fmt.Errorf("hunt: target %q has no fuzz driver (reuse not ready)", targetID)
		}
		return t.ID, t.Title, nil
	}
	return targetID, targetID, nil
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
