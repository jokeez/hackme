package poolsync

import (
	"math"
	"testing"

	"hackme/internal/chain"
)

func TestEscrowBudgetCoveringClaimV2PostFee(t *testing.T) {
	chain.ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(chain.ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "1")
	chain.ResetEconomicsV2ActivationHeightForTest()

	const price = 100.0
	need := EscrowBudgetCoveringClaim(price, 1)
	if need <= 0 || need >= price {
		t.Fatalf("v2 covering claim for %.2f = %.8f, want post-fee < price", price, need)
	}
	// Era-0 10% → 90 HMC escrow.
	if math.Abs(need-90.0) > 1e-6 {
		t.Fatalf("need=%.8f want 90", need)
	}
}

func TestMatchOpenHubEscrowV2PriceClaimPasses(t *testing.T) {
	chain.ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(chain.ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "1")
	chain.ResetEconomicsV2ActivationHeightForTest()

	const price = 60.0
	need := EscrowBudgetCoveringClaim(price, 1)
	row := &chain.FuzzEscrowRow{
		CampaignID: "camp-v2", BudgetHMC: need, BudgetRuns: 8, Status: "open",
	}
	fpPrice := EscrowPrepaidFingerprintParts("camp-v2", price, 8, "open")
	if err := MatchOpenHubEscrowForPrepaidAt(row, "camp-v2", price, 8, fpPrice, 1); err != nil {
		t.Fatalf("legitimate v2 escrow must pass mint-ticket coverage: %v", err)
	}
	fpRow := EscrowPrepaidFingerprintParts("camp-v2", need, 8, "open")
	if err := MatchOpenHubEscrowForPrepaidAt(row, "camp-v2", price, 8, fpRow, 1); err != nil {
		t.Fatalf("post-fee fingerprint must also pass: %v", err)
	}
	// Dust escrow must still fail a large price claim.
	dust := &chain.FuzzEscrowRow{CampaignID: "camp-v2", BudgetHMC: 1.0, BudgetRuns: 8, Status: "open"}
	if err := MatchOpenHubEscrowForPrepaidAt(dust, "camp-v2", price, 8, fpPrice, 1); err == nil {
		t.Fatal("dust escrow must not cover a 60 HMC price claim")
	}
}

func TestEscrowBudgetCoveringClaimPreV2(t *testing.T) {
	chain.ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(chain.ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "999999")
	chain.ResetEconomicsV2ActivationHeightForTest()

	const price = 100.0
	need := EscrowBudgetCoveringClaim(price, 100)
	if math.Abs(need-price) > 1e-9 {
		t.Fatalf("pre-v2 need=%.8f want %.8f", need, price)
	}
}
