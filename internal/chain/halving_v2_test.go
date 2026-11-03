package chain

import (
	"math"
	"testing"
)

func withV2From(t *testing.T, h string) {
	t.Helper()
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", h)
	ResetEconomicsV2ActivationHeightForTest()
}

// Exact v2 epoch boundaries: epoch = (index-1)/4 204 800 counted from genesis, so block
// k·interval is the last of epoch k-1 and k·interval+1 the first of epoch k.
func TestV2HalvingBoundaries(t *testing.T) {
	withV2From(t, "1")
	const iv = V2RewardHalvingIntervalBlocks
	cases := []struct {
		idx       uint64
		hmc, sup  float64
		burnRate  float64
		tailEpoch bool
	}{
		{1, 0.1, 0.002, 0.05, false},
		{iv, 0.1, 0.002, 0.05, false},
		{iv + 1, 0.05, 0.001, 0.025, false},
		{2 * iv, 0.05, 0.001, 0.025, false},
		{2*iv + 1, 0.025, 0.0005, 0.0125, false},
		{5*iv + 1, 0.003125, 0.002 / 32, 0.05 / 32, false},
		{6*iv + 1, 0.002, 0.002 / 64, 0.05 / 64, true}, // HMC tail floor from epoch 6
		{40 * iv, 0.002, 0, 0.05 / 64, true},           // SUP long stopped, burn frozen
	}
	for _, c := range cases {
		if got := BaseRewardForBlockIndex(c.idx); math.Abs(got-c.hmc) > 1e-15 {
			t.Fatalf("HMC at %d = %v want %v", c.idx, got, c.hmc)
		}
		if got := SUPRewardForBlockIndex(c.idx); math.Abs(got-c.sup) > 1e-15 {
			t.Fatalf("SUP at %d = %v want %v", c.idx, got, c.sup)
		}
		// Fee rules follow the next block: tip = idx-1.
		if got := OrderBurnRateAt(c.idx - 1); math.Abs(got-c.burnRate) > 1e-15 {
			t.Fatalf("burn at block %d = %v want %v", c.idx, got, c.burnRate)
		}
		if got := OrderPlatformFeeRateAt(c.idx - 1); got != V2OrderPlatformFeeRate {
			t.Fatalf("platform fee at %d = %v", c.idx, got)
		}
	}
}

// HMC never drops below the 0.002 tail; SUP has no tail and stops once a reward rounds below
// one unit; total v2 block SUP stays under the 21 000 cap even with the pre-v2 supply.
func TestV2EmissionTotals(t *testing.T) {
	withV2From(t, "1")
	const iv = V2RewardHalvingIntervalBlocks
	var supUnits uint64
	stopEpoch := -1
	for e := uint64(0); e < 64; e++ {
		first := e*iv + 1
		if BaseRewardForBlockIndex(first) < RewardTailFloorHMC {
			t.Fatalf("HMC below tail at epoch %d", e)
		}
		u := SUPToUnits(SUPRewardForBlockIndex(first))
		if u == 0 && stopEpoch < 0 {
			stopEpoch = int(e)
		}
		supUnits += u * iv
	}
	if stopEpoch < 0 || stopEpoch > 20 {
		t.Fatalf("SUP never stops (stop epoch %d)", stopEpoch)
	}
	total := UnitsToSUP(supUnits)
	const preV2Minted = 3400.0 // live supply incl. the Oct overmint, rounded up
	if total+preV2Minted > V2SUPMaxSupply {
		t.Fatalf("v2 block SUP %.4f + pre-v2 %.0f exceeds the %.0f cap", total, preV2Minted, V2SUPMaxSupply)
	}
	t.Logf("v2 block SUP total %.4f over %d emitting epochs (era 0: %.1f SUP); HMC era 0: %.0f HMC",
		total, stopEpoch, 0.002*float64(iv), 0.1*float64(iv))
}

// NextHalvingBlock is the first block of the next epoch — including when the next block itself
// starts it (tip = k·interval).
func TestNextHalvingBlockBoundaries(t *testing.T) {
	withV2From(t, "1")
	const iv = V2RewardHalvingIntervalBlocks
	for _, c := range []struct{ tip, want uint64 }{
		{0, iv + 1}, {515_000, iv + 1}, {iv - 1, iv + 1}, {iv, iv + 1}, {iv + 1, 2*iv + 1}, {2 * iv, 2*iv + 1},
	} {
		if got := NextHalvingBlock(c.tip); got != c.want {
			t.Fatalf("v2 NextHalvingBlock(%d)=%d want %d", c.tip, got, c.want)
		}
	}
	withV2From(t, "")
	const v1 = RewardHalvingIntervalBlocks
	for _, c := range []struct{ tip, want uint64 }{{0, v1 + 1}, {v1, v1 + 1}, {v1 + 1, 2*v1 + 1}} {
		if got := NextHalvingBlock(c.tip); got != c.want {
			t.Fatalf("v1 NextHalvingBlock(%d)=%d want %d", c.tip, got, c.want)
		}
	}
}
