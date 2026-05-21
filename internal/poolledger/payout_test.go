package poolledger

import (
	"strings"
	"testing"
)

func TestCheckTreasuryInvariantDriftFails(t *testing.T) {
	err := CheckTreasuryInvariant(1.0, 1.000002)
	if err == nil || !strings.Contains(err.Error(), LedgerDriftDetected) {
		t.Fatalf("want %s, got %v", LedgerDriftDetected, err)
	}
}

func TestComputeAttemptPayoutFoundOnly(t *testing.T) {
	p := ComputeAttemptPayout(1_000_000, 1.0, false, 0.25, true)
	if p != 0 {
		t.Fatalf("found-only non-hit want 0, got %v", p)
	}
}
