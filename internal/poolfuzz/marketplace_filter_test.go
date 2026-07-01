package poolfuzz

import "testing"

func TestIsInternalGateCampaign(t *testing.T) {
	cases := []struct {
		id, title, owner string
		cfg              map[string]any
		want             bool
	}{
		{"pool-sync-gate-123", "pool-sync-gate", "gate:pool-sync", nil, true},
		{"pool-sync-node-pool-sync-gate-1", "pool sync node gate", "", nil, true},
		{"warppool-probe-1", "probe", "", nil, true},
		{"campaign-gate-abc", "test", "", nil, true},
		{"campaign-audit-20260604", "gate-audit", "gate:foo", nil, true},
		{"campaign-audit-real", "Bitcoin guard audit", "HMC-abc", nil, false},
		{"l1v4-btc-1", "L1 v4 upstream Bitcoin guard", "", map[string]any{"internal_gate": true}, true},
		{"campaign-audit-20260701", "tier-gate-audit-1", "tier-gate:audit:1", nil, true},
		{"campaign-audit-20260701", "tier-debug-audit", "tier:audit:debug", nil, true},
	}
	for _, tc := range cases {
		got := IsInternalGateCampaign(tc.id, tc.title, tc.owner, tc.cfg)
		if got != tc.want {
			t.Errorf("IsInternalGateCampaign(%q,%q,%q)=%v want %v", tc.id, tc.title, tc.owner, got, tc.want)
		}
	}
}

func TestIsMarketplaceCampaign(t *testing.T) {
	if !IsMarketplaceCampaign("running", "campaign-audit-1", "Customer audit", "HMC-x", nil) {
		t.Fatal("expected real audit in marketplace")
	}
	if IsMarketplaceCampaign("cancelled", "campaign-audit-1", "Customer audit", "HMC-x", nil) {
		t.Fatal("cancelled must be hidden")
	}
	if IsMarketplaceCampaign("running", "pool-sync-gate-1", "pool-sync-gate", "gate:pool-sync", nil) {
		t.Fatal("gate must be hidden")
	}
}
