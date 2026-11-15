package poolsync

import (
	"math"
	"testing"

	"hackme/internal/chain"
)

// tipHeight==0 must NOT invent the activation height (that under-covered ~10% pre-fork).
// Unknown tip → conservative full claim; explicit post-activation tip → post-fee need.
func TestEscrowBudgetCoveringClaimTipZeroConservative(t *testing.T) {
	chain.ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(chain.ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "550000")
	chain.ResetEconomicsV2ActivationHeightForTest()

	const price = 100.0
	pre := EscrowBudgetCoveringClaim(price, 100)
	if math.Abs(pre-price) > 1e-9 {
		t.Fatalf("pre-fork tip=100 need=%.8f want %.8f", pre, price)
	}
	zero := EscrowBudgetCoveringClaim(price, 0)
	if math.Abs(zero-price) > 1e-9 {
		t.Fatalf("tipHeight=0 must require full claim, got %.8f want %.8f", zero, price)
	}
	post := EscrowBudgetCoveringClaim(price, 550000)
	if math.Abs(post-90.0) > 1e-6 {
		t.Fatalf("post-activation tip need=%.8f want 90", post)
	}
}
