package poolfuzz

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
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
	fallbackMiner := s.findingMinerAddress(ctx, campaignID)
	if fallbackMiner == "" {
		fallbackMiner = resolveWorkerPayoutAddress("")
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT COALESCE(NULLIF(miner_address,''), ?) FROM fuzz_work_items
		 WHERE campaign_id=? AND status='done' ORDER BY id ASC`, fallbackMiner, campaignID)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var miner string
		if err := rows.Scan(&miner); err != nil {
			return runs, findings, finalize, err
		}
		miner = strings.TrimSpace(miner)
		if miner == "" {
			continue
		}
		if err := s.EnqueueSettleOutbox(ctx, "run", campaignID, miner, ""); err != nil {
			return runs, findings, finalize, err
		}
		runs++
	}
	if err := rows.Err(); err != nil {
		return runs, findings, finalize, err
	}
	fRows, err := s.DB.QueryContext(ctx,
		`SELECT severity, detail_json FROM fuzz_findings WHERE campaign_id=?`, campaignID)
	if err != nil {
		return runs, findings, finalize, err
	}
	defer fRows.Close()
	for fRows.Next() {
		var sev, detail string
		if err := fRows.Scan(&sev, &detail); err != nil {
			return runs, findings, finalize, err
		}
		miner := minerFromFindingDetail(detail)
		if miner == "" {
			miner = fallbackMiner
		}
		if miner == "" {
			continue
		}
		if err := s.EnqueueSettleOutbox(ctx, "finding", campaignID, miner, sev); err != nil {
			return runs, findings, finalize, err
		}
		findings++
	}
	if err := s.EnqueueSettleOutbox(ctx, "finalize", campaignID, "", ""); err != nil {
		return runs, findings, finalize, err
	}
	finalize = 1
	return runs, findings, finalize, nil
}

func (s *Service) findingMinerAddress(ctx context.Context, campaignID string) string {
	var detail string
	err := s.DB.QueryRowContext(ctx,
		`SELECT detail_json FROM fuzz_findings WHERE campaign_id=? ORDER BY created_at DESC LIMIT 1`, campaignID).
		Scan(&detail)
	if err != nil {
		return ""
	}
	if miner := minerFromFindingDetail(detail); miner != "" {
		return miner
	}
	var m map[string]any
	if json.Unmarshal([]byte(detail), &m) == nil {
		if workerID, ok := m["worker_id"].(string); ok {
			return resolveWorkerPayoutAddress(workerID)
		}
	}
	return ""
}

func minerFromFindingDetail(detail string) string {
	var m map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(detail)), &m) != nil {
		return ""
	}
	if addr, ok := m["miner_address"].(string); ok {
		addr = strings.TrimSpace(addr)
		if strings.HasPrefix(addr, "HMC-") && len(addr) == 20 {
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
	if legacy := strings.TrimSpace(os.Getenv("FUZZ_LEGACY_MINER_ADDRESS")); strings.HasPrefix(legacy, "HMC-") && len(legacy) == 20 {
		if workerID == "" {
			return legacy
		}
	}
	raw := strings.TrimSpace(os.Getenv("WORKER_PAYOUT_MAP"))
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
				if strings.HasPrefix(addr, "HMC-") && len(addr) == 20 {
					return addr
				}
			}
		}
	}
	if legacy := strings.TrimSpace(os.Getenv("FUZZ_LEGACY_MINER_ADDRESS")); strings.HasPrefix(legacy, "HMC-") && len(legacy) == 20 {
		return legacy
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			addr := strings.TrimSpace(kv[1])
			if strings.HasPrefix(addr, "HMC-") && len(addr) == 20 {
				return addr
			}
		}
	}
	return ""
}
