package chain

import (
	"testing"

	"hackme/internal/fuzzescrow"
)

// Desktop / pool followers often have empty local blocks (tip 0). Without SetNetworkTipHeight,
// OpenFuzzEscrowWithFee would skip Economics v2 fees after network activation.
func TestOpenFuzzEscrowUsesNetworkTipForV2Fees(t *testing.T) {
	// Activation far ahead so local genesis tip stays pre-v2.
	c := newFeeTestChain(t, "500000", 400, 0)

	local := c.svc.localTipHeight(c.ctx)
	if EconomicsV2ActiveForIndex(local + 1) {
		t.Fatalf("test expects local tip pre-v2, got local=%d", local)
	}

	pre, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "fee-era-pre", 100, 100, fuzzescrow.EscrowSplit2080, FeeAssetHMC)
	if err != nil {
		t.Fatal(err)
	}
	if pre.BudgetHMC < 99.9 {
		t.Fatalf("pre-v2 escrow should keep ~full budget, got %.8f", pre.BudgetHMC)
	}

	c.svc.SetNetworkTipHeight(500_000) // network already past activation
	if got := c.svc.FeeEraTipHeight(c.ctx); got < 500_000 {
		t.Fatalf("FeeEraTipHeight=%d want >= 500000", got)
	}

	post, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "fee-era-post", 100, 100, fuzzescrow.EscrowSplit2080, FeeAssetHMC)
	if err != nil {
		t.Fatal(err)
	}
	// v2: 5% burn + 5% platform → escrow retains 90.
	if post.BudgetHMC < 89.9 || post.BudgetHMC > 90.1 {
		t.Fatalf("v2 escrow after network tip: got budget %.8f want ~90", post.BudgetHMC)
	}
}

func TestFeeEraTipHeightPrefersHigherNetwork(t *testing.T) {
	c := newFeeTestChain(t, "500000", 10, 0)
	if got := c.svc.FeeEraTipHeight(c.ctx); got != c.svc.localTipHeight(c.ctx) {
		t.Fatalf("no network tip: got %d", got)
	}
	c.svc.SetNetworkTipHeight(123)
	c.svc.SetNetworkTipHeight(100) // must not regress
	if got := c.svc.FeeEraTipHeight(c.ctx); got != 123 {
		t.Fatalf("got %d want 123", got)
	}
	c.svc.SetNetworkTipHeight(550_050)
	if got := c.svc.FeeEraTipHeight(c.ctx); got != 550_050 {
		t.Fatalf("got %d want 550050", got)
	}
}
