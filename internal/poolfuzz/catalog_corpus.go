package poolfuzz

import (
	"context"
	"database/sql"
	"strings"

	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
)

// catalogSharedTarget is the catalog id whose on-disk seed cache is shared
// across campaigns. A second catalog order on mpack should start from units
// the first order already proved, not from "{}".
//
// Inventory and customer-harness orders are not catalog. Their inputs stay
// inside that campaign: writing them into the shared cache would hand one
// customer's bytes to the next order on the same target id.
func catalogSharedTarget(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	source := strings.ToLower(strings.TrimSpace(jsonString(cfg["hunt_source"])))
	if source != "catalog" {
		return ""
	}
	id := strings.TrimSpace(jsonString(cfg["upstream_target_id"]))
	clean, ok := fuzzupstream.SanitizeCatalogID(id)
	if !ok {
		return ""
	}
	return clean
}

// sharesResearchNamespace reports whether coverage bytes may land in the
// research:<target> table. Inventory orders stay campaign-local: that table is
// listed by target id, so a customer input labeled mpack would otherwise be
// visible to the next campaign on the same id.
func sharesResearchNamespace(cfg map[string]any) bool {
	if cfg == nil {
		return true
	}
	return strings.ToLower(strings.TrimSpace(jsonString(cfg["hunt_source"]))) != "inventory"
}

// mergeCatalogSeedCache pulls the shared catalog seed directory into cfg when
// its generation changed, and stores that generation on the campaign. The
// next claim then stats the directory and does not read the files again.
// Inventory and customer harness orders are not catalog, so they return first.
func (s *Service) mergeCatalogSeedCache(ctx context.Context, campaignID string, cfg map[string]any) error {
	targetID := catalogSharedTarget(cfg)
	if targetID == "" || cfg == nil {
		return nil
	}
	root := hunt.RepoRoot()
	gen, err := hunt.SharedSeedGeneration(root, targetID)
	if err != nil {
		return err
	}
	if jsonString(cfg["catalog_seed_gen"]) == gen {
		return nil
	}
	if s == nil || s.DB == nil || strings.TrimSpace(campaignID) == "" {
		if err := absorbCatalogSeeds(cfg, root, targetID); err != nil {
			return err
		}
		cfg["catalog_seed_gen"] = gen
		return nil
	}
	return s.withCampaignSettle(campaignID, func() error {
		var cfgJSON string
		err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgJSON)
		if err == sql.ErrNoRows {
			if merr := absorbCatalogSeeds(cfg, root, targetID); merr != nil {
				return merr
			}
			cfg["catalog_seed_gen"] = gen
			return nil
		}
		if err != nil {
			return err
		}
		fresh := parseConfigJSON(cfgJSON)
		genNow, err := hunt.SharedSeedGeneration(root, targetID)
		if err != nil {
			return err
		}
		if jsonString(fresh["catalog_seed_gen"]) != genNow {
			if err := absorbCatalogSeeds(fresh, root, targetID); err != nil {
				return err
			}
			fresh["catalog_seed_gen"] = genNow
			if _, err := s.DB.ExecContext(ctx, `UPDATE fuzz_campaigns SET config_json=? WHERE id=?`, marshalConfigJSON(fresh), campaignID); err != nil {
				return err
			}
		}
		if raw, ok := fresh["seed_byte_corpus"]; ok {
			cfg["seed_byte_corpus"] = raw
		}
		if g := jsonString(fresh["catalog_seed_gen"]); g != "" {
			cfg["catalog_seed_gen"] = g
		}
		return nil
	})
}

// CatalogLeaseSeedWindow is the live corpus a leased catalog shard may pull
// before its wall ends. A matching generation is one stat: the file bytes are
// not read. Inventory and customer harnesses return no seeds. The caller must
// already hold the work-item lease.
func (s *Service) CatalogLeaseSeedWindow(ctx context.Context, workerID, campaignID string, itemID int64, haveGen string, shift uint64) (gen string, seeds [][]byte, unchanged bool, err error) {
	if s == nil || s.DB == nil {
		return "", nil, true, nil
	}
	if err := s.requireResearchLease(ctx, workerID, campaignID, itemID); err != nil {
		return "", nil, false, err
	}
	var cfgJSON string
	if err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgJSON); err != nil {
		return "", nil, false, err
	}
	cfg := parseConfigJSON(cfgJSON)
	targetID := catalogSharedTarget(cfg)
	if targetID == "" {
		return "", nil, true, nil
	}
	root := hunt.RepoRoot()
	gen, err = hunt.SharedSeedGeneration(root, targetID)
	if err != nil {
		return "", nil, false, err
	}
	if gen == strings.TrimSpace(haveGen) {
		return gen, nil, true, nil
	}
	seeds, err = hunt.SampleLibFuzzerSeedWindow(root, targetID, shift, 0)
	if err != nil {
		return gen, nil, false, err
	}
	return gen, seeds, false, nil
}

// spreadCatalogClaimSeeds fills this claim from the on-disk cache. The campaign
// config keeps the newest fold; the claim adds a shard-specific older slice
// when the cache holds more than one wire budget. Inventory is not catalog,
// so this reads nothing. The window is not written back: the next claim with
// the same input_n rebuilds it, and a different input_n takes the next slice.
func spreadCatalogClaimSeeds(cfg map[string]any, inputN uint64) {
	targetID := catalogSharedTarget(cfg)
	if targetID == "" || cfg == nil {
		return
	}
	seeds, err := hunt.SampleLibFuzzerSeedWindow(hunt.RepoRoot(), targetID, inputN, 0)
	if err != nil || len(seeds) == 0 {
		return
	}
	cfg["seed_byte_corpus"] = foldSharedSeedCorpus(cfg["seed_byte_corpus"], seeds)
}

// absorbCatalogSeeds reads the shared cache and folds it into cfg. The stamp
// is the caller's job, and only after this returns, so a generation is never
// marked seen before its bytes are in the campaign corpus.
func absorbCatalogSeeds(cfg map[string]any, root, targetID string) error {
	seeds, err := hunt.LoadLibFuzzerSeedFiles(hunt.LibFuzzerSeedDir(root, targetID), 0)
	if err != nil {
		return err
	}
	if len(seeds) == 0 {
		return nil
	}
	seeds = hunt.RankLibFuzzerSeeds(seeds, 0)
	cfg["seed_byte_corpus"] = foldSharedSeedCorpus(cfg["seed_byte_corpus"], seeds)
	return nil
}
