package chain

import "testing"

// FuzzOrderFeeV2ForEscrow asserts the pricing helper never panics (fimasw Low 2026-11-08).
func FuzzOrderFeeV2ForEscrow(f *testing.F) {
	f.Add(uint64(10), uint64(900_000_000), "HMC")
	f.Add(uint64(550_000), uint64(15_679_732_462_653_118_874), "HMC")
	f.Add(uint64(1), ^uint64(0), "sup")
	f.Fuzz(func(t *testing.T, tip, escrow uint64, asset string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("panic tip=%d escrow=%d asset=%q: %v", tip, escrow, asset, r)
			}
		}()
		_, _ = OrderFeeV2ForEscrow(tip, escrow, asset)
	})
}

// FuzzOrderFeeMonotonic: larger escrow never yields a smaller HMC fee at the same tip.
func FuzzOrderFeeMonotonic(f *testing.F) {
	f.Add(uint64(10), uint64(1_000_000), uint64(2_000_000))
	f.Fuzz(func(t *testing.T, tip, a, b uint64) {
		if a > b {
			a, b = b, a
		}
		fa, errA := OrderFeeV2ForEscrow(tip, a, FeeAssetHMC)
		fb, errB := OrderFeeV2ForEscrow(tip, b, FeeAssetHMC)
		if errA != nil || errB != nil {
			return
		}
		if fa.HMCUnits() > fb.HMCUnits() {
			t.Fatalf("fee non-monotonic: tip=%d a=%d fee=%d b=%d fee=%d", tip, a, fa.HMCUnits(), b, fb.HMCUnits())
		}
	})
}
