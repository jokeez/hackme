package hms

import (
	"testing"
)

// Locked alignment with internal/chain.OrderPlatformFeeRate and OrderBurnRate (kernel sheet).
const (
	chainOrderPlatformFeeRate = 0.05
	chainOrderBurnRate        = 0.10
)

func TestMarketPolicyMatchesChainKernel(t *testing.T) {
	if MarketPlatformFeeRate != chainOrderPlatformFeeRate {
		t.Fatalf("platform fee drift: hms=%v kernel=%v", MarketPlatformFeeRate, chainOrderPlatformFeeRate)
	}
	if MarketBurnRate != chainOrderBurnRate {
		t.Fatalf("burn rate drift: hms=%v kernel=%v", MarketBurnRate, chainOrderBurnRate)
	}
	if err := validateMarketPolicy(); err != nil {
		t.Fatal(err)
	}
	h1 := marketPolicyHash()
	h2 := marketPolicyHash()
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("policy hash unstable: %q", h1)
	}
}

func TestQuoteStorageOrderDeterministic(t *testing.T) {
	q1, err := QuoteStorageOrder(1<<30, 30)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := VerifyQuoteHash(1<<30, 30, q1.QuoteHash)
	if err != nil {
		t.Fatal(err)
	}
	if q1.TotalDebitHMC != q2.TotalDebitHMC {
		t.Fatalf("total mismatch %v vs %v", q1.TotalDebitHMC, q2.TotalDebitHMC)
	}
	if q1.TotalDebitHMC < MarketMinPrepaidHMC {
		t.Fatalf("below min prepaid: %v", q1.TotalDebitHMC)
	}
	if q1.PolicyHash != marketPolicyHash() {
		t.Fatal("policy hash not embedded in quote")
	}
}

func TestQuoteRejectsTamperedHash(t *testing.T) {
	q, _ := QuoteStorageOrder(10<<20, 30)
	if _, err := VerifyQuoteHash(10<<20, 30, q.QuoteHash+"ff"); err == nil {
		t.Fatal("expected hash mismatch")
	}
}
