package main

import (
	"testing"

	"hackme/internal/chain"
	"hackme/internal/workerfuzzloop"
)

func TestPayoutIsTreasuryRefuse(t *testing.T) {
	if chain.DevFeeAddress == "" {
		t.Fatal("DevFeeAddress unset")
	}
	if !workerfuzzloop.PayoutIsTreasury(chain.DevFeeAddress) {
		t.Fatal("exact treasury address must be refused")
	}
	if !workerfuzzloop.PayoutIsTreasury("  " + chain.DevFeeAddress + "  ") {
		t.Fatal("trimmed treasury must be refused")
	}
	if workerfuzzloop.PayoutIsTreasury("HMC-deadbeefdeadbeef") {
		t.Fatal("non-treasury address must be allowed")
	}
}
