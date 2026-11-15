package chain

import (
	"testing"

	"hackme/internal/hms"
)

func TestHMSMarketRatesMatchChainKernel(t *testing.T) {
	if hms.MarketPlatformFeeRate != OrderPlatformFeeRate && hms.MarketPlatformFeeRate != V2OrderPlatformFeeRate {
		t.Fatalf("platform fee: hms=%v chain=%v v2=%v", hms.MarketPlatformFeeRate, OrderPlatformFeeRate, V2OrderPlatformFeeRate)
	}
	// Post Economics v2 activation, HMS market burn tracks the Dig/Hunt era-0 burn (5%),
	// not the legacy pre-v2 OrderBurnRate (10%).
	if hms.MarketBurnRate != V2OrderBurnRate {
		t.Fatalf("burn rate: hms=%v want v2=%v (legacy OrderBurnRate=%v)", hms.MarketBurnRate, V2OrderBurnRate, OrderBurnRate)
	}
}
