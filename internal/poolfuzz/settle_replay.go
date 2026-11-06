package poolfuzz

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"hackme/internal/chain"
	"hackme/internal/fuzzengine"
	"hackme/internal/fuzznative"
	"hackme/internal/poolauth"
)

// ReplayCampaignSettles enqueues pending run/finding/finalize rows for a completed campaign.
// Legacy campaigns without per-item miner_address use the finding miner's payout address for all runs.
func (s *Service) ReplayCampaignSettles(ctx context.Context, campaignID string) (runs, findings, finalize int, err error) {
	if s == nil || s.DB == nil {
		return 0, 0, 0, fmt.Errorf("poolfuzz: no database")
	}
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return 0, 0, 0, fmt.Errorf("poolfuzz: campaign_id required")
	}
	var done int
	if err := s.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM fuzz_work_items WHERE campaign_id=? AND status='done'`, campaignID).Scan(&done); err != nil {
		return 0, 0, 0, err
	}
	if done == 0 {
		return 0, 0, 0, fmt.Errorf("poolfuzz: no completed work for %s", campaignID)
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id, COALESCE(miner_address,''), COALESCE(settle_run_status,'') FROM fuzz_work_items
		 WHERE campaign_id=? AND status='done' ORDER BY id ASC`, campaignID)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var itemID int64
		var miner, runSt string
		if err := rows.Scan(&itemID, &miner, &runSt); err != nil {
			return runs, findings, finalize, err
		}
		miner = strings.TrimSpace(miner)
		runSt = strings.TrimSpace(strings.ToLower(runSt))
		// Never attribute empty work-item miners to the finding winner (H-04).
		if miner == "" || !chain.IsValidAddress(miner) {
			continue
		}
		kind := "run"
		switch runSt {
		case "paid":
			continue // already settled on origin
		case "pending_floor", "queued_floor":
			// Hunt CLEAN floor — must not replay as full PayRun (econ overpay).
			kind = "run_floor"
		case "pending", "queued":
			kind = "run"
		case "":
			// Never invent PayRun for force-complete / legacy rows with no stamp.
			continue
		default:
			continue
		}
		if _, err := s.EnqueueSettleOutbox(ctx, kind, campaignID, miner, "", itemID); err != nil {
			return runs, findings, finalize, err
		}
		runs++
	}
	if err := rows.Err(); err != nil {
		return runs, findings, finalize, err
	}
	cfg, err := s.CampaignConfig(ctx, campaignID)
	if err != nil {
		return runs, findings, finalize, err
	}
	fRows, err := s.DB.QueryContext(ctx,
		`SELECT id, severity, detail_json FROM fuzz_findings WHERE campaign_id=? ORDER BY created_at ASC`, campaignID)
	if err != nil {
		return runs, findings, finalize, err
	}
	defer fRows.Close()
	for fRows.Next() {
		var findingID, sev, detail string
		if err := fRows.Scan(&findingID, &sev, &detail); err != nil {
			return runs, findings, finalize, err
		}
		if !huntBountyEligible(cfg, sev) {
			continue
		}
		if cfg != nil && fuzzengine.BountyRequiresNative(cfg) {
			ok, err := fuzznative.IsFindingNativeEligibleForBounty(ctx, s.DB, findingID)
			if err != nil || !ok {
				continue
			}
		}
		miner := minerFromFindingDetail(detail)
		if miner == "" {
			_, itemID := minerAndItemFromFindingDetail(detail)
			miner = s.workItemMinerAddress(ctx, campaignID, itemID)
		}
		if miner == "" {
			continue
		}
		if _, err := s.EnqueueSettleOutbox(ctx, "finding", campaignID, miner, sev, 0); err != nil {
			return runs, findings, finalize, err
		}
		findings++
		break // bounty pool pays first qualifying finder only
	}
	if _, err := s.EnqueueSettleOutbox(ctx, "finalize", campaignID, "", "", 0); err != nil {
		return runs, findings, finalize, err
	}
	finalize = 1
	return runs, findings, finalize, nil
}

func minerFromFindingDetail(detail string) string {
	var m map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(detail)), &m) != nil {
		return ""
	}
	if addr, ok := m["miner_address"].(string); ok {
		addr = strings.TrimSpace(addr)
		if chain.IsValidAddress(addr) {
			return addr
		}
	}
	if workerID, ok := m["worker_id"].(string); ok {
		return resolveWorkerPayoutAddress(workerID)
	}
	return ""
}

func resolveWorkerPayoutAddress(workerID string) string {
	workerID = strings.TrimSpace(workerID)
	if legacy := strings.TrimSpace(os.Getenv("FUZZ_LEGACY_MINER_ADDRESS")); chain.IsValidAddress(legacy) {
		if workerID == "" {
			return legacy
		}
	}
	raw := poolauth.WorkerPayoutMapRaw()
	if workerID != "" {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				continue
			}
			if strings.TrimSpace(kv[0]) == workerID {
				addr := strings.TrimSpace(kv[1])
				if chain.IsValidAddress(addr) {
					return addr
				}
			}
		}
	}
	if legacy := strings.TrimSpace(os.Getenv("FUZZ_LEGACY_MINER_ADDRESS")); chain.IsValidAddress(legacy) {
		return legacy
	}
	// Fail closed: never fall back to "first address in WORKER_PAYOUT_MAP" (wrong-pay risk).
	return ""
}
