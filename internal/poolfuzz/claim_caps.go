package poolfuzz

import (
	"context"
	"sync"
	"time"
)

var pendingHuntCache struct {
	mu sync.Mutex
	at time.Time
	n  int
}

// PendingHuntItems returns how many running/planned Hunt campaigns still have pending work
// (cached 10s). Used only to explain an empty claim to a Hunt-incapable worker.
func (s *Service) PendingHuntItems(ctx context.Context) (int, error) {
	pendingHuntCache.mu.Lock()
	if time.Since(pendingHuntCache.at) < 10*time.Second {
		n := pendingHuntCache.n
		pendingHuntCache.mu.Unlock()
		return n, nil
	}
	pendingHuntCache.mu.Unlock()
	// Same runnable set Claim uses (pool-distributed, not an internal gate campaign).
	customers, rest, err := s.runnablePoolCampaignIDsByTier(ctx, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	var ids []string
	for _, id := range append(customers, rest...) {
		var cfgJSON string
		if err := s.DB.QueryRowContext(ctx, `SELECT config_json FROM fuzz_campaigns WHERE id=?`, id).Scan(&cfgJSON); err != nil {
			continue
		}
		if IsHuntCampaign(parseConfigJSON(cfgJSON)) {
			ids = append(ids, id)
		}
	}
	n := len(s.filterIDsWithPendingWork(ctx, ids))
	pendingHuntCache.mu.Lock()
	pendingHuntCache.at, pendingHuntCache.n = time.Now(), n
	pendingHuntCache.mu.Unlock()
	return n, nil
}
