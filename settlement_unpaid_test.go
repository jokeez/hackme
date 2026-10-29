package main

import "testing"

func TestSettlementUnpaidHMCSessionAnchor(t *testing.T) {
	// A11-related: unpaid uses session_anchor when set (anti double-pay after rebase).
	ent := workerSettlementStateEntry{SettledHMC: 10, SessionAnchorHMC: 8}
	if got := settlementUnpaidHMC(9, ent); got != 1 {
		t.Fatalf("unpaid=%v want 1 (above anchor)", got)
	}
	if got := settlementUnpaidHMC(7, ent); got != 0 {
		t.Fatalf("unpaid=%v want 0 (below anchor)", got)
	}
	ent2 := workerSettlementStateEntry{SettledHMC: 5}
	if got := settlementUnpaidHMC(8, ent2); got != 3 {
		t.Fatalf("unpaid=%v want 3", got)
	}
}

func TestSettlementUnpaidSUPSessionAnchor(t *testing.T) {
	ent := workerSettlementStateEntry{SettledSUP: 100, SessionAnchorSUP: 90}
	if got := settlementUnpaidSUP(95, ent); got != 5 {
		t.Fatalf("unpaid=%v want 5", got)
	}
}
