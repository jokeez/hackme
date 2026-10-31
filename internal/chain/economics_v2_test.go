package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestEconomicsV2DisabledByDefault(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "")
	ResetEconomicsV2ActivationHeightForTest()
	if EconomicsV2ActivationHeight() != 0 {
		t.Fatalf("want 0 unset, got %d", EconomicsV2ActivationHeight())
	}
	if EconomicsV2ActiveForIndex(1_000_000) {
		t.Fatal("v2 must stay off when activation unset")
	}
	if got := BaseRewardForBlockIndex(1); got != InitialBaseRewardHMC {
		t.Fatalf("v1 reward: got %v want %v", got, InitialBaseRewardHMC)
	}
	if got := OrderBurnRateAt(0); got != OrderBurnRate {
		t.Fatalf("v1 burn: got %v", got)
	}
	if got := OrderPlatformFeeRateAt(0); got != OrderPlatformFeeRate {
		t.Fatalf("v1 fee: got %v", got)
	}
	if got := SUPRewardForBlockIndex(1); got != 0 {
		t.Fatalf("SUP before v2: got %v", got)
	}
}

func TestEconomicsV2ActivationHeightEnv(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "600000")
	ResetEconomicsV2ActivationHeightForTest()
	if got := EconomicsV2ActivationHeight(); got != 600_000 {
		t.Fatalf("got %d", got)
	}
	if EconomicsV2ActiveForIndex(599_999) {
		t.Fatal("below activation")
	}
	if !EconomicsV2ActiveForIndex(600_000) {
		t.Fatal("at activation")
	}
}

func TestEconomicsV2RewardsAndFees(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "100")
	ResetEconomicsV2ActivationHeightForTest()

	if got := BaseRewardForBlockIndex(99); got != InitialBaseRewardHMC {
		t.Fatalf("pre-v2 reward %v", got)
	}
	if got := BaseRewardForBlockIndex(100); got != V2InitialBaseRewardHMC {
		t.Fatalf("v2 reward %v want %v", got, V2InitialBaseRewardHMC)
	}
	// tip 99 → tip+1=100 activates v2 fee schedule for new orders
	if got := OrderBurnRateAt(99); got != V2OrderBurnRate {
		t.Fatalf("burn at tip99 %v", got)
	}
	if got := OrderPlatformFeeRateAt(99); got != V2OrderPlatformFeeRate {
		t.Fatalf("fee at tip99 %v", got)
	}
	if got := OrderBurnRateAt(50); got != OrderBurnRate {
		t.Fatalf("burn pre %v", got)
	}
	if got := SUPRewardForBlockIndex(100); got != V2SUPInitialReward {
		t.Fatalf("SUP %v", got)
	}
	if got := SUPRewardForBlockIndex(99); got != 0 {
		t.Fatalf("SUP pre %v", got)
	}
}

func TestEconomicsV2HalvingInterval(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "1")
	ResetEconomicsV2ActivationHeightForTest()
	// First era still 0.1 until 4_204_800 blocks from genesis epoch math.
	if got := BaseRewardForBlockIndex(1); got != 0.1 {
		t.Fatalf("era0 %v", got)
	}
	halfAt := V2RewardHalvingIntervalBlocks + 1
	want := V2InitialBaseRewardHMC / 2
	if got := BaseRewardForBlockIndex(halfAt); got != want {
		t.Fatalf("era1 got %v want %v", got, want)
	}
	nh := NextHalvingBlock(100)
	if nh != V2RewardHalvingIntervalBlocks+1 {
		t.Fatalf("next halving %d", nh)
	}
}

func TestEconomicsV2EmptyBlockMintsSUP(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "1")
	ResetEconomicsV2ActivationHeightForTest()

	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	addr := "HMC-testnode"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	nonce, eval := firstPoHHit(m)
	before, err := svc.supTotalMintedUnits(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AppendPoHBlock(ctx, addr, nonce, eval, BaseRewardForBlockIndex(1), m, ""); err != nil {
		t.Fatal(err)
	}
	after, err := svc.supTotalMintedUnits(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	want := SUPToUnits(V2SUPInitialReward)
	if after != before+want {
		t.Fatalf("SUP minted: before=%d after=%d want +%d", before, after, want)
	}
	st, err := svc.SupAddressState(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if st.BalanceSUPUnits < want {
		t.Fatalf("wallet SUP %d want >= %d", st.BalanceSUPUnits, want)
	}
}
