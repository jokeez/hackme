package poolfuzz

import "context"

// Settler relays micro-payments on the chain node (20% run pool, 80% bounty pool).
type Settler interface {
	PayRun(ctx context.Context, campaignID, minerAddress string) error
	PayFinding(ctx context.Context, campaignID, minerAddress, severity string) error
	Finalize(ctx context.Context, campaignID string) error
}

// NoopSettler skips settlement (local gate tests without a node wallet).
type NoopSettler struct{}

func (NoopSettler) PayRun(context.Context, string, string) error      { return nil }
func (NoopSettler) PayFinding(context.Context, string, string, string) error { return nil }
func (NoopSettler) Finalize(context.Context, string) error             { return nil }

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
