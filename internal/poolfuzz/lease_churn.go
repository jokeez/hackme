package poolfuzz

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// maxLeaseChurnAttempts is how many expired-lease reclaims a work item may survive
// before the coordinator force-completes it so near-done campaigns cannot pin the
// marketplace forever (lease → vanish → reclaim → lease loops).
func maxLeaseChurnAttempts() int {
	v := strings.TrimSpace(os.Getenv("HACKME_POOL_LEASE_CHURN_MAX_ATTEMPTS"))
	if v == "" {
		return 8
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 2 {
		return 8
	}
	if n > 64 {
		return 64
	}
	return n
}

// stalledNearDoneSec: when a running campaign has almost finished (≤2 remaining)
// but no work item progressed for this long, force-complete leftovers.
func stalledNearDoneSec() int64 {
	v := strings.TrimSpace(os.Getenv("HACKME_POOL_STALLED_NEAR_DONE_SEC"))
	if v == "" {
		return 1800 // 30m
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n < 120 {
		return 1800
	}
	if n > 86400 {
		return 86400
	}
	return n
}

// ForceCompleteLeaseChurnItems marks work items that exceeded reclaim churn as done
// so budget-based campaigns can finalize. result_ok=1 when Hunt already proved crashes
// or showmap edges on the shard (honest "work happened"); otherwise result_ok=0 with
// lease_churn_exhausted.
func (s *Service) ForceCompleteLeaseChurnItems(ctx context.Context, now int64, limit int) (int, error) {
	if s == nil || s.DB == nil {
		return 0, nil
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	maxAtt := maxLeaseChurnAttempts()
	rows, err := s.DB.QueryContext(ctx,
		`SELECT w.id, w.campaign_id, COALESCE(w.hunt_proven_crashes,0), COALESCE(w.hunt_proven_edges,0), COALESCE(w.status,'')
		 FROM fuzz_work_items w
		 JOIN fuzz_campaigns c ON c.id=w.campaign_id
		 WHERE c.status IN ('planned','running')
		   AND w.status IN ('pending','leased','replay_pending')
		   AND w.attempts >= ?
		 ORDER BY w.updated_at ASC
		 LIMIT ?`, maxAtt, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type hit struct {
		itemID  int64
		campID  string
		crashes int
		edges   int
	}
	var hits []hit
	for rows.Next() {
		var h hit
		var status string
		if err := rows.Scan(&h.itemID, &h.campID, &h.crashes, &h.edges, &status); err != nil {
			return 0, err
		}
		hits = append(hits, h)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, h := range hits {
		pass := h.crashes > 0 || h.edges > 0
		if err := s.forceCompleteWorkItem(ctx, h.campID, h.itemID, pass, "lease_churn_exhausted", now); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// ForceCompleteStalledNearDoneCampaigns completes leftover shards when a campaign is
// one-or-two runs from budget and has made no progress for stalledNearDoneSec.
// This is the marketplace "31/32 forever / ETA ~1s" failure mode.
func (s *Service) ForceCompleteStalledNearDoneCampaigns(ctx context.Context, now int64, limit int) (int, error) {
	if s == nil || s.DB == nil {
		return 0, nil
	}
	if now <= 0 {
		now = time.Now().Unix()
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	staleSec := stalledNearDoneSec()
	rows, err := s.DB.QueryContext(ctx,
		`SELECT c.id, c.budget_runs,
		        COALESCE((SELECT COUNT(*) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status='done'),0) AS done_n,
		        COALESCE((SELECT MAX(w.updated_at) FROM fuzz_work_items w WHERE w.campaign_id=c.id AND w.status='done'),0) AS last_done
		 FROM fuzz_campaigns c
		 WHERE c.status='running'
		   AND c.budget_runs > 0
		   AND json_extract(c.config_json, '$.pool_distributed') IN (1, 'true', '1')
		 ORDER BY c.created_at ASC
		 LIMIT ?`, limit*4)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type camp struct {
		id       string
		budget   int
		done     int
		lastDone int64
	}
	var camps []camp
	for rows.Next() {
		var c camp
		if err := rows.Scan(&c.id, &c.budget, &c.done, &c.lastDone); err != nil {
			return 0, err
		}
		remaining := c.budget - c.done
		if remaining < 1 || remaining > 2 {
			continue
		}
		if c.lastDone <= 0 || now-c.lastDone < staleSec {
			continue
		}
		camps = append(camps, c)
		if len(camps) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, c := range camps {
		itemRows, err := s.DB.QueryContext(ctx,
			`SELECT id, COALESCE(hunt_proven_crashes,0), COALESCE(hunt_proven_edges,0) FROM fuzz_work_items
			 WHERE campaign_id=? AND status IN ('pending','leased','replay_pending')`, c.id)
		if err != nil {
			return n, err
		}
		type leftover struct {
			id      int64
			crashes int
			edges   int
		}
		var left []leftover
		for itemRows.Next() {
			var L leftover
			if err := itemRows.Scan(&L.id, &L.crashes, &L.edges); err != nil {
				itemRows.Close()
				return n, err
			}
			left = append(left, L)
		}
		itemRows.Close()
		if err := itemRows.Err(); err != nil {
			return n, err
		}
		for _, L := range left {
			pass := L.crashes > 0 || L.edges > 0
			if err := s.forceCompleteWorkItem(ctx, c.id, L.id, pass, "stalled_near_done_force_complete", now); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

func (s *Service) forceCompleteWorkItem(ctx context.Context, campaignID string, itemID int64, pass bool, reason string, now int64) error {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "force_complete"
	}
	res, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_work_items
		 SET status='done', attempts=attempts+1, result_ok=?, duration_ms=CASE WHEN duration_ms>0 THEN duration_ms ELSE 0 END,
		     last_error=?, lease_owner='', lease_until=0, updated_at=?
		 WHERE id=? AND campaign_id=? AND status IN ('pending','leased','replay_pending')`,
		boolToInt(pass), reason, now, itemID, campaignID)
	if err != nil {
		return err
	}
	aff, _ := res.RowsAffected()
	if aff == 0 {
		return nil
	}
	// Drop any stale hunt replay row so async workers do not resurrect the shard.
	if _, err := s.DB.ExecContext(ctx,
		`UPDATE fuzz_hunt_replay_queue SET status='failed', last_error=?, updated_at=?
		 WHERE campaign_id=? AND item_id=? AND status IN ('pending','processing')`,
		reason, now, campaignID, itemID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such table") {
		return err
	}

	var cfgJSON string
	_ = s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, campaignID).Scan(&cfgJSON)
	cfg := parseConfigJSON(cfgJSON)
	// Flush PayRun/PayFinding before campaign Finalize so force-complete does not
	// strand settle outbox rows (audit tip 3415c68 H2: forceComplete→Finalize sans PayRun).
	if s.Settler != nil && escrowEnabled(cfg) {
		if err := s.flushPendingSettles(ctx, campaignID, itemID, cfg); err != nil {
			return fmt.Errorf("poolfuzz: flush settle after %s: %w", reason, err)
		}
	}
	completed, err := s.recomputeProgress(ctx, campaignID, now)
	if err != nil {
		return err
	}
	if completed && s.Settler != nil && escrowEnabled(cfg) {
		if err := s.tryFinalizeEscrow(ctx, campaignID); err != nil {
			return fmt.Errorf("poolfuzz: finalize after %s: %w", reason, err)
		}
	}
	s.clearEmptyClaimCache()
	return nil
}
