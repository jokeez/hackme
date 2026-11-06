package chain

import (
	"errors"
	"testing"

	"hackme/internal/fuzzescrow"
)

// A create that fails after the escrow opened must cost the customer nothing (escrow + v2 fee).
func TestRollbackUnstartedFuzzEscrowRefundsV2Fee(t *testing.T) {
	c := newFeeTestChain(t, "1", 200, HMCToUnits(20))
	supBurned := func() uint64 { v, _ := c.svc.metaUint(c.ctx, c.db, metaSUPTotalBurnedUnits, 0); return v }
	h0, s0, d0, b0 := c.balances(t)
	sb0 := supBurned()
	if _, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "rb-hmc", 100, 100, fuzzescrow.EscrowSplit2080, "HMC"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "rb-sup", 100, 100, fuzzescrow.EscrowSplit5050, "SUP"); err != nil {
		t.Fatal(err)
	}
	h1, _, d1, b1 := c.balances(t)
	if h1 >= h0 || d1 <= d0 || b1 <= b0 {
		t.Fatalf("open did not charge: wallet %d->%d dev %d->%d burned %d->%d", h0, h1, d0, d1, b0, b1)
	}
	for _, id := range []string{"rb-hmc", "rb-sup"} {
		if err := c.svc.RollbackUnstartedFuzzEscrow(c.ctx, id); err != nil {
			t.Fatalf("rollback %s: %v", id, err)
		}
	}
	h2, s2, d2, b2 := c.balances(t)
	if h2 != h0 || s2 != s0 || d2 != d0 || b2 != b0 || supBurned() != sb0 {
		t.Fatalf("not fully undone: HMC %d->%d SUP %d->%d dev %d->%d burned %d->%d supBurned %d->%d",
			h0, h2, s0, s2, d0, d2, b0, b2, sb0, supBurned())
	}
	var n int
	_ = c.db.QueryRow(`SELECT COUNT(*) FROM fuzz_campaign_escrow WHERE campaign_id IN ('rb-hmc','rb-sup')`).Scan(&n)
	if n != 0 {
		t.Fatalf("escrow rows left: %d", n)
	}
	// The same id can be created again (SUP fee ref no longer blocks it).
	if _, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "rb-sup", 100, 100, fuzzescrow.EscrowSplit5050, "SUP"); err != nil {
		t.Fatalf("re-create after rollback: %v", err)
	}
}

// Once anything was paid the escrow cannot be undone as if unopened.
func TestRollbackRefusedAfterPayout(t *testing.T) {
	c := newFeeTestChain(t, "1", 200, HMCToUnits(20))
	if _, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "rb-paid", 100, 100, fuzzescrow.EscrowSplit2080, "HMC"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.svc.PayFuzzRun(c.ctx, "rb-paid", "HMC-1111111111111111"); err != nil {
		t.Fatal(err)
	}
	if err := c.svc.RollbackUnstartedFuzzEscrow(c.ctx, "rb-paid"); !errors.Is(err, ErrFuzzEscrowStarted) {
		t.Fatalf("want ErrFuzzEscrowStarted, got %v", err)
	}
}
