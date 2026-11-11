package main

import "testing"

func TestChainCommandLedgerBlocksP2PReplay(t *testing.T) {
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "")
	if _, blocked := chainCommandLedgerBlocksP2PReplay(); blocked {
		t.Fatal("default follower must allow replay policy path")
	}
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "1")
	if reason, blocked := chainCommandLedgerBlocksP2PReplay(); !blocked || reason == "" {
		t.Fatalf("LOCAL_POH command node must block, got blocked=%v reason=%q", blocked, reason)
	}
	t.Setenv("HACKME_CHAIN_LEADER_LOCAL_POH", "0")
	t.Setenv("HACKME_CHAIN_MINTING_HUB", "1")
	if reason, blocked := chainCommandLedgerBlocksP2PReplay(); !blocked {
		t.Fatalf("minting hub with LOCAL_POH=0 must still block state replay, reason=%q", reason)
	}
}
