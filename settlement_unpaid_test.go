package main

import "testing"

func TestSettlementUnpaidHMCSessionAnchor(t *testing.T) {
	// Rebase (settled > accrued): unpaid is growth above session_anchor.
	ent := workerSettlementStateEntry{SettledHMC: 10, SessionAnchorHMC: 8}
	if got := settlementUnpaidHMC(9, ent); got != 1 {
		t.Fatalf("unpaid=%v want 1 (above anchor)", got)
	}
	if got := settlementUnpaidHMC(7, ent); got != 0 {
		t.Fatalf("unpaid=%v want 0 (would re-anchor to accrued)", got)
	}
	ent2 := workerSettlementStateEntry{SettledHMC: 5}
	if got := settlementUnpaidHMC(8, ent2); got != 3 {
		t.Fatalf("unpaid=%v want 3", got)
	}
	// Caught up: stale session_anchor must not inflate unpaid (matches settler ClearAnchor).
	caught := workerSettlementStateEntry{SettledHMC: 500, SessionAnchorHMC: 490}
	if got := settlementUnpaidHMC(500, caught); got != 0 {
		t.Fatalf("caught-up unpaid=%v want 0", got)
	}
	if got := settlementUnpaidHMC(508, caught); got != 8 {
		t.Fatalf("growth unpaid=%v want 8", got)
	}
}

func TestSettlementUnpaidSUPSessionAnchor(t *testing.T) {
	// Rebase path only.
	ent := workerSettlementStateEntry{SettledSUP: 100, SessionAnchorSUP: 90}
	if got := settlementUnpaidSUP(95, ent); got != 5 {
		t.Fatalf("unpaid=%v want 5", got)
	}
	caught := workerSettlementStateEntry{SettledSUP: 100, SessionAnchorSUP: 90}
	if got := settlementUnpaidSUP(100, caught); got != 0 {
		t.Fatalf("caught-up unpaid=%v want 0", got)
	}
}
