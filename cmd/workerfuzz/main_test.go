package main

import (
	"runtime"
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

func TestHuntHarnessExecForStandaloneDigOnlyWithoutEngines(t *testing.T) {
	t.Setenv("HACKME_HUNT_HARNESS_EXEC", "")
	// On Windows DetectHuntEngines is empty without clang/bwrap; same on most CI Linux
	// runners without Hunt toolchains. Explicit env still wins.
	if got := huntHarnessExecForStandalone(); got != "" && runtime.GOOS == "windows" {
		t.Fatalf("Windows Dig-only want empty harness, got %q", got)
	}
	t.Setenv("HACKME_HUNT_HARNESS_EXEC", "custom_harness")
	if got := huntHarnessExecForStandalone(); got != "custom_harness" {
		t.Fatalf("explicit env must win, got %q", got)
	}
}
