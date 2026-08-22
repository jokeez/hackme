package poolfuzz

import (
	"context"
	"fmt"
)

// LocalDrainCampaign runs claim/execute/submit until budgetRuns complete.
// Unlike prod workers, it tops up the queue via EnsureWorkItems when the pending
// batch is drained (queue_depth caps each EnsureWorkItems batch).
func (s *Service) LocalDrainCampaign(ctx context.Context, campaignID string, budgetRuns int, workerIDs []string, now int64, runOne func(workerID string, w ClaimedWork) error) (int, error) {
	if s == nil || s.DB == nil {
		return 0, fmt.Errorf("poolfuzz: no database")
	}
	if budgetRuns <= 0 {
		return 0, nil
	}
	if len(workerIDs) == 0 {
		workerIDs = []string{"local-worker-1"}
	}
	done := 0
	for done < budgetRuns {
		progress := false
		for _, wid := range workerIDs {
			if done >= budgetRuns {
				break
			}
			w, ok, err := s.Claim(ctx, wid, now)
			if err != nil {
				return done, err
			}
			if !ok || w.CampaignID != campaignID {
				continue
			}
			if err := runOne(wid, w); err != nil {
				return done, err
			}
			done++
			progress = true
		}
		if progress {
			continue
		}
		if err := s.EnsureWorkItems(ctx, campaignID, now); err != nil {
			return done, err
		}
		retried := false
		for _, wid := range workerIDs {
			if done >= budgetRuns {
				break
			}
			w, ok, err := s.Claim(ctx, wid, now)
			if err != nil {
				return done, err
			}
			if !ok || w.CampaignID != campaignID {
				continue
			}
			if err := runOne(wid, w); err != nil {
				return done, err
			}
			done++
			retried = true
		}
		if !retried {
			break
		}
	}
	return done, nil
}
