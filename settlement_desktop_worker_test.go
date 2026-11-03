package main

import "testing"

func TestResolveDesktopSettlementWorkerIDPrefixMatch(t *testing.T) {
	a := &app{dataDir: t.TempDir(), nodeID: "HMC-91fe007e4036c602"}
	workers := map[string]any{
		"worker-kapa-pc": map[string]any{
			"payout_hmc":     100.0,
			"payout_address": "HMC-91fe007e4036c602",
		},
		"other-rig": map[string]any{
			"payout_hmc":     1.0,
			"payout_address": "HMC-other",
		},
	}
	// DefaultDesktop-style id misses exact key; prefix should pick worker-kapa-pc.
	got := a.resolveDesktopSettlementWorkerID(workers, "worker-kapa")
	if got != "worker-kapa-pc" {
		t.Fatalf("got %q want worker-kapa-pc", got)
	}
}

func TestResolveDesktopSettlementWorkerIDExactPreferred(t *testing.T) {
	a := &app{dataDir: t.TempDir(), nodeID: "HMC-aaa"}
	workers := map[string]any{
		"worker-kapa": map[string]any{"payout_hmc": 10.0, "payout_address": "HMC-aaa"},
		"worker-kapa-pc": map[string]any{"payout_hmc": 99.0, "payout_address": "HMC-aaa"},
	}
	got := a.resolveDesktopSettlementWorkerID(workers, "worker-kapa")
	if got != "worker-kapa" {
		t.Fatalf("exact configured id must win, got %q", got)
	}
}

func TestResolveDesktopSettlementWorkerIDByPayoutAddress(t *testing.T) {
	a := &app{dataDir: t.TempDir(), nodeID: "HMC-91fe007e4036c602"}
	workers := map[string]any{
		"fleet-gpu-7": map[string]any{
			"payout_hmc":     50.0,
			"payout_address": "HMC-91fe007e4036c602",
		},
	}
	got := a.resolveDesktopSettlementWorkerID(workers, "worker-nope")
	if got != "fleet-gpu-7" {
		t.Fatalf("got %q want fleet-gpu-7 via payout address", got)
	}
}
