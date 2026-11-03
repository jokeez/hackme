package chain

import (
	"testing"

	"hackme/internal/block"
	"hackme/internal/fuzzescrow"
)

// End-to-end activation on a lab network: a guarded hub (confirm + lead) mines v1 blocks, crosses
// the height into v2, and a follower with its own wallet replays the whole chain. Checks the
// reward switch, SUP start, header rules on both sides, order fees on both sides, and that both
// nodes end with identical balances for every block-derived account.
func TestV2ForkCrossesActivationEndToEnd(t *testing.T) {
	const act = 5
	n := newV2Net(t, "5")
	guardOn(t)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM", "5")
	t.Setenv("HACKME_ECONOMICS_V2_MIN_LEAD_BLOCKS", "3") // lab: 5 - next(1) ≥ 3
	fundAddr(t, n.hubDB, n.signer.Address(), HMCToUnits(5))
	if _, err := n.hubDB.Exec(`UPDATE wallet SET balance_units=balance_units+?, balance_hmc=balance_hmc+? WHERE id=1`, HMCToUnits(300), 300.0); err != nil {
		t.Fatal(err)
	}
	if _, err := n.hubDB.Exec(`UPDATE accounts SET balance_units=balance_units+?, balance_sup_units=? WHERE address=?`, HMCToUnits(300), HMCToUnits(50), n.wallet); err != nil {
		t.Fatal(err)
	}
	if _, err := n.hubDB.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+?`, metaTotalMintedUnits, HMCToUnits(305), HMCToUnits(305)); err != nil {
		t.Fatal(err)
	}

	var blocks []*block.Block
	for i := 0; i < act-2; i++ { // blocks 1..3: v1, next block (4) still v1
		blocks = append(blocks, n.mine())
	}
	if lockedV2Height(t, n.hub, n.hubDB) != act {
		t.Fatal("confirmed height not locked on first block")
	}
	// v1 side: SUP fee refused, HMC fee legacy (no real burn).
	if _, err := n.hub.OpenFuzzEscrowWithFee(n.ctx, "dig-pre", 100, 100, fuzzescrow.EscrowSplit2080, "SUP"); err == nil {
		t.Fatal("SUP fee accepted before activation")
	}
	pre, err := n.hub.OpenFuzzEscrowWithFee(n.ctx, "dig-pre", 100, 100, fuzzescrow.EscrowSplit2080, "HMC")
	if err != nil {
		t.Fatal(err)
	}
	blocks = append(blocks, n.mine()) // block 4: last v1; fee rules now follow block 5 (v2)
	for i := 0; i < 3; i++ {          // blocks 5..7: v2
		blocks = append(blocks, n.mine())
	}
	post, err := n.hub.OpenFuzzEscrowWithFee(n.ctx, "dig-post", 100, 100, fuzzescrow.EscrowSplit2080, "SUP")
	if err != nil {
		t.Fatalf("v2 SUP-fee open: %v", err)
	}
	if HMCToUnits(pre.RunsPoolHMC+pre.BountyPoolHMC) != HMCToUnits(100) || HMCToUnits(post.RunsPoolHMC+post.BountyPoolHMC) != HMCToUnits(90) {
		t.Fatalf("escrow pre=%v+%v post=%v+%v", pre.RunsPoolHMC, pre.BountyPoolHMC, post.RunsPoolHMC, post.BountyPoolHMC)
	}

	for _, b := range blocks {
		v2 := b.Index >= act
		if v2 != (b.ChainID == V2ChainID && b.RewardTo == n.wallet) || (!v2 && (b.ChainID != "" || b.RewardTo != "")) {
			t.Fatalf("block %d header chain_id=%q reward_to=%q", b.Index, b.ChainID, b.RewardTo)
		}
		if want := BaseRewardForBlockIndex(b.Index); (v2 && want != 0.1) || (!v2 && want != 0.01) {
			t.Fatalf("block %d reward %v", b.Index, want)
		}
	}
	// Follower replays the whole lab chain across the height.
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM", "")
	f, fdb := n.follower()
	for _, b := range blocks {
		if err := f.ImportPoHBlock(n.ctx, b); err != nil {
			t.Fatalf("follower import %d: %v", b.Index, err)
		}
	}
	// Block-derived: SUP minted only by v2 blocks, to reward_to; identical on both nodes.
	_, hubSUP := unitsOf(t, n.hubDB, n.wallet)
	_, folSUP := unitsOf(t, fdb, n.wallet)
	wantSUP := uint64(3) * SUPToUnits(V2SUPInitialReward)
	if folSUP != wantSUP {
		t.Fatalf("follower SUP %d want %d (3 v2 blocks)", folSUP, wantSUP)
	}
	if hubSUP != HMCToUnits(50)+wantSUP-HMCToUnits(10) { // +3 blocks, −10 SUP fee for dig-post
		t.Fatalf("hub SUP %d", hubSUP)
	}
	fh, _ := unitsOf(t, fdb, n.wallet)
	wantHMC := uint64(3) * HMCToUnits(0.1) // v2 rewards land on the signed reward_to
	if fh != wantHMC {
		t.Fatalf("follower HMC for reward_to %d want %d", fh, wantHMC)
	}
	if h, s := unitsOf(t, fdb, v2FollowerWallet); h != uint64(4)*HMCToUnits(0.01) || s != 0 {
		// v1 legacy: followers still credit their local wallet for pre-activation blocks.
		t.Fatalf("follower local wallet %d HMC / %d SUP (want only the 4 v1 rewards)", h, s)
	}
	fhTip, fTip, _ := f.Tip(n.ctx)
	hhTip, hTip, _ := n.hub.Tip(n.ctx)
	if fhTip != hhTip || fTip != hTip {
		t.Fatalf("tips differ: follower %d %s hub %d %s", fhTip, fTip, hhTip, hTip)
	}
}
