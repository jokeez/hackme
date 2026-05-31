package chain

import (
	"testing"

	"hackme/internal/hms"
)

func TestHMSMarketRatesMatchChainKernel(t *testing.T) {
	if hms.MarketPlatformFeeRate != OrderPlatformFeeRate {
		t.Fatalf("platform fee: hms=%v chain=%v", hms.MarketPlatformFeeRate, OrderPlatformFeeRate)
	}
	if hms.MarketBurnRate != OrderBurnRate {
		t.Fatalf("burn rate: hms=%v chain=%v", hms.MarketBurnRate, OrderBurnRate)
	}
}
