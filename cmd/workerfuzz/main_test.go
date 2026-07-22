package main

import (
	"testing"

	"hackme/internal/chain"
)

func TestPayoutIsTreasuryRefuse(t *testing.T) {
	if chain.DevFeeAddress == "" {
		t.Fatal("DevFeeAddress unset")
	}
	if !payoutIsTreasury(chain.DevFeeAddress) {
		t.Fatal("exact treasury address must be refused")
	}
	if !payoutIsTreasury("  " + chain.DevFeeAddress + "  ") {
		t.Fatal("trimmed treasury must be refused")
	}
	if payoutIsTreasury("HMC-deadbeefdeadbeef") {
		t.Fatal("non-treasury address must be allowed")
	}
}
