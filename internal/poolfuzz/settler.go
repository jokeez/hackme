package poolfuzz

import "context"

// SettleResult reports whether origin confirmed apply, or only durable-queued the event.
type SettleResult struct {
	OutboxID int64
	Applied  bool
}

// Settler relays micro-payments on the chain node (20% run pool, 80% bounty pool).
// reuseOutboxID>0 retries the same durable event_id (no second enqueue / no double-pay).
type Settler interface {
	PayRun(ctx context.Context, campaignID, minerAddress string, reuseOutboxID int64) (SettleResult, error)
	PayFinding(ctx context.Context, campaignID, minerAddress, severity string, reuseOutboxID int64) (SettleResult, error)
	Finalize(ctx context.Context, campaignID string, reuseOutboxID int64) (SettleResult, error)
}

// NoopSettler skips settlement (local gate tests without a node wallet).
type NoopSettler struct{}

func (NoopSettler) PayRun(context.Context, string, string, int64) (SettleResult, error) {
	return SettleResult{Applied: true}, nil
}
func (NoopSettler) PayFinding(context.Context, string, string, string, int64) (SettleResult, error) {
	return SettleResult{Applied: true}, nil
}
func (NoopSettler) Finalize(context.Context, string, int64) (SettleResult, error) {
	return SettleResult{Applied: true}, nil
}

func escrowEnabled(cfg map[string]any) bool {
	if v, ok := cfg["budget_hmc"]; ok {
		switch x := v.(type) {
		case float64:
			return x > 0
		case int:
			return x > 0
		}
	}
	return false
}
