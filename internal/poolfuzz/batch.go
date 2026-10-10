package poolfuzz

import (
	"context"
	"fmt"
	"strings"
)

// MaxBatchClaimSubmit caps optional batch claim/submit APIs (lease + auth safety).
const MaxBatchClaimSubmit = 16

// BatchSubmitItem is one result row from SubmitBatch.
type BatchSubmitItem struct {
	ItemID       int64         `json:"item_id"`
	CampaignID   string        `json:"campaign_id,omitempty"`
	OK           bool          `json:"ok"`
	Error        string        `json:"error,omitempty"`
	ReplayStatus string        `json:"replay_status,omitempty"`
	Outcome      SubmitOutcome `json:"-"`
}

// ClaimBatch leases up to n work items for the same worker (n capped at MaxBatchClaimSubmit).
// Stops early when the queue is empty. Each item keeps independent lease_owner semantics.
func (s *Service) ClaimBatch(ctx context.Context, workerID string, now int64, n int) ([]ClaimedWork, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return nil, fmt.Errorf("poolfuzz: worker_id required")
	}
	if n < 1 {
		n = 1
	}
	if n > MaxBatchClaimSubmit {
		n = MaxBatchClaimSubmit
	}
	out := make([]ClaimedWork, 0, n)
	for i := 0; i < n; i++ {
		w, ok, err := s.Claim(ctx, workerID, now)
		if err != nil {
			return out, err
		}
		if !ok {
			break
		}
		out = append(out, w)
	}
	return out, nil
}

// SubmitBatch submits each request independently. A foreign lease_owner fails that
// row only — other items still process (no cross-worker lease steal).
func (s *Service) SubmitBatch(ctx context.Context, reqs []SubmitRequest) []BatchSubmitItem {
	if len(reqs) > MaxBatchClaimSubmit {
		reqs = reqs[:MaxBatchClaimSubmit]
	}
	out := make([]BatchSubmitItem, 0, len(reqs))
	for _, req := range reqs {
		item := BatchSubmitItem{
			ItemID:     req.ItemID,
			CampaignID: strings.TrimSpace(req.CampaignID),
		}
		outcome, err := s.SubmitWithOutcome(ctx, req)
		if err != nil {
			item.OK = false
			item.Error = err.Error()
			out = append(out, item)
			continue
		}
		item.OK = true
		item.Outcome = outcome
		item.ReplayStatus = outcome.ReplayStatus
		out = append(out, item)
	}
	return out
}
