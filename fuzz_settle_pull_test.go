package main

import "testing"

func TestFuzzSettleOutboxAction(t *testing.T) {
	cases := []struct {
		status       string
		kind         string
		wantApply    bool
		wantDrain    bool
	}{
		{"open", "run", true, false},
		{"open", "finding", true, false},
		{"closed", "run", false, true},
		{"bounty_paid", "run", false, true},
		{"bounty_paid", "finding", false, true},
		{"bounty_paid", "finalize", true, false},
		{"unknown", "run", false, false},
	}
	for _, tc := range cases {
		apply, drain := fuzzSettleOutboxAction(tc.status, tc.kind)
		if apply != tc.wantApply || drain != tc.wantDrain {
			t.Errorf("fuzzSettleOutboxAction(%q,%q) = (%v,%v) want (%v,%v)",
				tc.status, tc.kind, apply, drain, tc.wantApply, tc.wantDrain)
		}
	}
}
