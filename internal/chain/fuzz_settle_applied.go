package chain

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FuzzSettleEventID builds a stable globally unique key for a coordinator settle-outbox row.
// Format: outbox:<campaign_id>:<outbox_id>
//
// Legacy keys were outbox:<outbox_id> only. That collided across campaigns (and across
// coordinator DB resets) when customer nodes already had bootstrap-applied outbox:1..N —
// ApplyFuzzSettleOnce no-op'd while pull still ACKed, leaving escrow unpaid.
func FuzzSettleEventID(campaignID string, outboxID int64) string {
	campaignID = strings.TrimSpace(campaignID)
	if campaignID == "" {
		return fmt.Sprintf("outbox:%d", outboxID)
	}
	return fmt.Sprintf("outbox:%s:%d", campaignID, outboxID)
}

// MarkFuzzSettleApplied inserts a durable applied-event record.
// Returns (true, nil) when this call newly reserved the event; (false, nil) if already applied.
func (s *Service) MarkFuzzSettleApplied(ctx context.Context, eventID, campaignID, kind string) (newly bool, err error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("chain: no database")
	}
	eventID = strings.TrimSpace(eventID)
	campaignID = strings.TrimSpace(campaignID)
	kind = strings.TrimSpace(strings.ToLower(kind))
	if eventID == "" || campaignID == "" || kind == "" {
		return false, fmt.Errorf("chain: settle applied requires event_id, campaign_id, kind")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().Unix()
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO fuzz_settle_applied (event_id, campaign_id, kind, applied_at) VALUES (?, ?, ?, ?)`,
		eventID, campaignID, kind, now)
	if err != nil {
		return false, err
	}
	aff, _ := res.RowsAffected()
	return aff > 0, nil
}

// UnmarkFuzzSettleApplied removes an applied-event reservation after a transient pay failure.
func (s *Service) UnmarkFuzzSettleApplied(ctx context.Context, eventID string) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("chain: no database")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(ctx, `DELETE FROM fuzz_settle_applied WHERE event_id=?`, eventID)
	return err
}

// HasFuzzSettleApplied reports whether event_id was already durably recorded.
func (s *Service) HasFuzzSettleApplied(ctx context.Context, eventID string) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("chain: no database")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false, nil
	}
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM fuzz_settle_applied WHERE event_id=?`, eventID).Scan(&n)
	return n > 0, err
}
