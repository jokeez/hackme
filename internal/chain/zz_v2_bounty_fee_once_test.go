package chain

import (
	"testing"

	"hackme/internal/fuzzescrow"
)

// fimasw (Medium): under Economics v2 the 5% platform fee is taken out of the price at
// open, yet the bounty payout charged 5% of the (already net) bounty pool again and
// credited it to DevFeeAddress — the finder lost it. A v1 order settled after the fork
// must keep v1 terms (5% at payout).
func TestV2BountyPlatformFeeChargedOnce(t *testing.T) {
	const act = 5
	n := newV2Net(t, "5")
	guardOn(t)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_CONFIRM", "5")
	t.Setenv("HACKME_ECONOMICS_V2_MIN_LEAD_BLOCKS", "3")
	fundAddr(t, n.hubDB, n.signer.Address(), HMCToUnits(5))
	if _, err := n.hubDB.Exec(`UPDATE wallet SET balance_units=balance_units+?, balance_hmc=balance_hmc+? WHERE id=1`, HMCToUnits(600), 600.0); err != nil {
		t.Fatal(err)
	}
	if _, err := n.hubDB.Exec(`UPDATE accounts SET balance_units=balance_units+? WHERE address=?`, HMCToUnits(600), n.wallet); err != nil {
		t.Fatal(err)
	}
	if _, err := n.hubDB.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=CAST(value AS INTEGER)+?`, metaTotalMintedUnits, HMCToUnits(605), HMCToUnits(605)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < act-2; i++ {
		n.mine()
	}
	// v1-priced order, settled after the fork.
	pre, err := n.hub.OpenFuzzEscrowWithFee(n.ctx, "bounty-pre", 100, 100, fuzzescrow.EscrowSplit2080, "HMC")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		n.mine()
	}
	dig, err := n.hub.OpenFuzzEscrowWithFee(n.ctx, "bounty-dig-v2", 100, 100, fuzzescrow.EscrowSplit2080, "HMC")
	if err != nil {
		t.Fatal(err)
	}
	hunt, err := n.hub.OpenFuzzEscrowWithFee(n.ctx, "bounty-hunt-v2", 100, 100, fuzzescrow.EscrowSplit5050, "HMC")
	if err != nil {
		t.Fatal(err)
	}

	pay := func(cid, miner, sev string) (minerGot, devGot uint64) {
		t.Helper()
		m0, _ := unitsOf(t, n.hubDB, miner)
		d0, _ := unitsOf(t, n.hubDB, DevFeeAddress)
		if _, err := n.hub.PayFuzzBounty(n.ctx, cid, miner, sev); err != nil {
			t.Fatalf("%s bounty: %v", cid, err)
		}
		m1, _ := unitsOf(t, n.hubDB, miner)
		d1, _ := unitsOf(t, n.hubDB, DevFeeAddress)
		return m1 - m0, d1 - d0
	}

	// v2 Dig: finder gets the whole remaining bounty pool, no second platform fee.
	got, dev := pay("bounty-dig-v2", "HMC-00000000000000d1", "critical")
	if want := HMCToUnits(dig.BountyPoolHMC); got != want || dev != 0 {
		t.Fatalf("v2 Dig: finder %d (want %d), dev fee at payout %d (want 0)", got, want, dev)
	}
	// v2 Hunt critical: full severity share, no fee slice.
	got, dev = pay("bounty-hunt-v2", "HMC-00000000000000d2", "critical")
	if want := HMCToUnits(hunt.BountyPoolHMC); got != want || dev != 0 {
		t.Fatalf("v2 Hunt: finder %d (want %d), dev fee at payout %d (want 0)", got, want, dev)
	}
	// v1 order settled after the fork: 5% at payout, as when it was opened.
	got, dev = pay("bounty-pre", "HMC-00000000000000d3", "critical")
	wantMiner, wantFee := fuzzescrow.BountyPayoutUnits(HMCToUnits(pre.BountyPoolHMC))
	if got != wantMiner || dev != wantFee || dev == 0 {
		t.Fatalf("v1 order after fork: finder %d fee %d, want %d / %d", got, dev, wantMiner, wantFee)
	}
}
