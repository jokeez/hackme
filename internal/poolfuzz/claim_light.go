package poolfuzz

import (
	"context"
	"fmt"
	"os"
	"strings"

	"hackme/internal/fuzzengine"
)

// claimLightEnabled prefers corpus_snapshot_sha256 (+ harness_hash) over fat corpus_seeds
// in claim JSON when an object store (or explicit opt-in) is available.
// Default ON when HACKME_POOL_CORPUS_DIR / SetCorpusObjectDir is set; force with
// HACKME_POOL_CLAIM_LIGHT=1; disable with =0.
func claimLightEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_POOL_CLAIM_LIGHT")))
	switch v {
	case "0", "false", "no", "off":
		return false
	case "1", "true", "yes", "on":
		return true
	}
	return CorpusObjectDir() != ""
}

// ShouldOmitFatCorpusSeeds reports whether claim HTTP should drop corpus_seeds[]
// and keep only corpus_snapshot_sha256 (worker fetches under lease).
func ShouldOmitFatCorpusSeeds(sha string, seeds []fuzzengine.PoolCorpusSeed) bool {
	if !claimLightEnabled() {
		return false
	}
	if strings.TrimSpace(sha) == "" {
		return false
	}
	if len(seeds) == 0 {
		return false
	}
	return true
}

// PoolExecCapTruth documents effective Dig exec/unit vs configured package depth.
type PoolExecCapTruth struct {
	Package      string `json:"package"`
	Configured   int    `json:"configured_exec_per_unit"`
	HubCap       int    `json:"hub_cap"`
	Effective    int    `json:"effective_exec_per_unit"`
	CappedOnPool bool   `json:"capped_on_pool"`
	HonestyNote  string `json:"honesty_note,omitempty"`
}

// CorpusSnapshotForLease returns frozen corpus seeds only for the active lease owner.
func (s *Service) CorpusSnapshotForLease(ctx context.Context, workerID, campaignID string, itemID int64) (seeds []fuzzengine.PoolCorpusSeed, sha string, err error) {
	if s == nil || s.DB == nil {
		return nil, "", fmt.Errorf("poolfuzz: no database")
	}
	workerID = strings.TrimSpace(workerID)
	campaignID = strings.TrimSpace(campaignID)
	if workerID == "" || campaignID == "" || itemID <= 0 {
		return nil, "", fmt.Errorf("poolfuzz: worker_id, campaign_id, item_id required")
	}
	var owner string
	err = s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(lease_owner,'') FROM fuzz_work_items
		 WHERE id=? AND campaign_id=? AND status='leased'`, itemID, campaignID).Scan(&owner)
	if err != nil {
		return nil, "", err
	}
	if owner != workerID {
		return nil, "", fmt.Errorf("poolfuzz: corpus snapshot requires active lease")
	}
	seeds, err = s.loadCorpusSnapshot(ctx, campaignID, itemID)
	if err != nil {
		return nil, "", err
	}
	if len(seeds) == 0 {
		return seeds, "", nil
	}
	_, sha, err = fuzzengine.EncodeCorpusSnapshot(seeds)
	return seeds, sha, err
}

// DigPoolExecTruth returns honest pool depth for Dig packages under current env cap.
func DigPoolExecTruth(cfg map[string]any) PoolExecCapTruth {
	pkg := digPackageKey(cfg)
	configured := fuzzengine.ExecPerUnit(cfg)
	capN := effectivePoolExecPerUnitCap()
	effective := PoolExecPerUnit(cfg)
	out := PoolExecCapTruth{
		Package:      pkg,
		Configured:   configured,
		HubCap:       capN,
		Effective:    effective,
		CappedOnPool: poolDistributed(cfg) && configured > effective,
	}
	if out.CappedOnPool {
		out.HonestyNote = "hub pool Deep/Audit exec_per_unit is capped by HACKME_POOL_EXEC_PER_UNIT_CAP; local autorunner may run full package depth"
	}
	return out
}
