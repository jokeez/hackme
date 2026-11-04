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
	rows, err := s.DB.QueryContext(ctx, `SELECT id, config_json FROM fuzz_campaigns WHERE status IN ('planned','running')`)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id, cfgJSON string
		if err := rows.Scan(&id, &cfgJSON); err != nil {
			rows.Close()
			return 0, err
		}
		if IsHuntCampaign(parseConfigJSON(cfgJSON)) {
			ids = append(ids, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := len(s.filterIDsWithPendingWork(ctx, ids))
	pendingHuntCache.mu.Lock()
	pendingHuntCache.at, pendingHuntCache.n = time.Now(), n
	pendingHuntCache.mu.Unlock()
	return n, nil
}
