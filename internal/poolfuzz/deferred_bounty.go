package poolfuzz

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzznative"
)

// flushDeferredBounties pays the first native-confirmed qualifying finding per campaign
// when bounty_requires_native blocked payout at submit time.
func (s *Service) flushDeferredBounties(ctx context.Context) error {
	if s == nil || s.DB == nil || s.Settler == nil {
		return nil
	}
	rows, err := s.DB.QueryContext(ctx,
		`SELECT f.id, f.campaign_id, f.severity, f.detail_json
		 FROM fuzz_findings f
		 INNER JOIN fuzz_native_queue n ON n.finding_id = f.id
		   AND n.status IN ('confirmed', 'native_crash')
		 WHERE lower(f.severity) IN ('medium', 'high', 'critical')
		   AND NOT EXISTS (
		     SELECT 1 FROM fuzz_settle_outbox o
		     WHERE o.campaign_id = f.campaign_id AND o.kind = 'finding' AND o.status = 'applied'
		   )
		 ORDER BY f.created_at ASC
		 LIMIT 20`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var findingID, campaignID, severity, detailJSON string
		if err := rows.Scan(&findingID, &campaignID, &severity, &detailJSON); err != nil {
			return err
		}
		cfg, err := s.CampaignConfig(ctx, campaignID)
		if err != nil || cfg == nil || !escrowEnabled(cfg) {
			continue
		}
		if fuzzengine.BountyRequiresNative(cfg) {
			ok, err := fuzznative.IsFindingNativeEligibleForBounty(ctx, s.DB, findingID)
			if err != nil || !ok {
				continue
			}
		}
		if !huntBountyEligible(cfg, severity) {
			continue
		}
		miner, itemID := minerAndItemFromFindingDetail(detailJSON)
		if miner == "" {
			miner = s.findingMinerAddress(ctx, campaignID)
		}
		if miner == "" {
			continue
		}
		if itemID > 0 {
			_, _ = s.DB.ExecContext(ctx,
				`UPDATE fuzz_work_items
				 SET settle_finding_status='pending', settle_finding_severity=?,
				     miner_address=CASE WHEN miner_address='' THEN ? ELSE miner_address END
				 WHERE campaign_id=? AND id=? AND settle_finding_status=''`,
				strings.TrimSpace(severity), miner, campaignID, itemID)
			if err := s.flushPendingSettles(ctx, campaignID, itemID, cfg); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "already paid") {
					continue
				}
				return err
			}
			continue
		}
		if _, err := s.Settler.PayFinding(ctx, campaignID, miner, severity, 0, 0); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "already paid") {
				continue
			}
			return err
		}
	}
	return rows.Err()
}

func minerAndItemFromFindingDetail(detailJSON string) (miner string, itemID int64) {
	var m map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(detailJSON)), &m) != nil {
		return "", 0
	}
	if addr, ok := m["miner_address"].(string); ok {
		miner = strings.TrimSpace(addr)
	}
	if miner == "" {
		if workerID, ok := m["worker_id"].(string); ok {
			miner = resolveWorkerPayoutAddress(workerID)
		}
	}
	switch x := m["item_id"].(type) {
	case float64:
		itemID = int64(x)
	case int64:
		itemID = x
	case int:
		itemID = int64(x)
	}
	return miner, itemID
}

// findingMinerAddress is used by settle replay and deferred bounty flush.
func (s *Service) findingMinerAddress(ctx context.Context, campaignID string) string {
	var detail string
	err := s.DB.QueryRowContext(ctx,
		`SELECT detail_json FROM fuzz_findings WHERE campaign_id=? ORDER BY created_at DESC LIMIT 1`, campaignID).
		Scan(&detail)
	if err == sql.ErrNoRows || err != nil {
		return ""
	}
	miner, _ := minerAndItemFromFindingDetail(detail)
	if miner != "" {
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
