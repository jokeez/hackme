package chain

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"hackme/internal/nodecrypto"
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

func TestEconomicsV2ActivationLocksToMeta(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "50")
	ResetEconomicsV2ActivationHeightForTest()

	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-testnode"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.lockEconomicsV2ActivationTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	got, err := svc.metaUint(ctx, db, metaEconomicsV2ActivationHeight, 0)
	if err != nil || got != 50 {
		t.Fatalf("meta height=%d err=%v", got, err)
	}
	// Disagreement after lock must fail.
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "99")
	tx2, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = svc.lockEconomicsV2ActivationTx(ctx, tx2)
	_ = tx2.Rollback()
	if err == nil {
		t.Fatal("expected env/meta disagreement error")
	}
}

func TestEconomicsV2InvariantAllowsMintPastV1Cap(t *testing.T) {
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
	// Pretend minted already above v1 100M cap; v2 must not trip invariant.
	over := HMCToUnits(MaxSupplyHMC) + HMCToUnits(1)
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = ?`, strconv.FormatUint(over, 10), metaTotalMintedUnits); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE meta SET value = ? WHERE key = ?`, strconv.FormatFloat(UnitsToHMC(over), 'f', -1, 64), metaTotalMintedHMC); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.lockEconomicsV2ActivationTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if err := svc.checkEconomicInvariants(ctx, tx); err != nil {
		t.Fatalf("v2 should skip max-supply invariant: %v", err)
	}
	_ = tx.Rollback()
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
	signer, err := nodecrypto.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWithSigner(db, signer) // v2 blocks must carry the authority signature
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

func TestEconomicsV2BurnHalvesThenFreezes(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "1")
	ResetEconomicsV2ActivationHeightForTest()

	if got := OrderBurnRateAt(0); got != V2OrderBurnRate {
		t.Fatalf("era0 burn %v", got)
	}
	halfTip := V2RewardHalvingIntervalBlocks // tip+1 starts era 1
	wantHalf := V2OrderBurnRate / 2
	if got := OrderBurnRateAt(halfTip); got != wantHalf {
		t.Fatalf("era1 burn got %v want %v", got, wantHalf)
	}
	freeze := v2BurnFreezeEpoch()
	if freeze < 1 {
		t.Fatalf("freeze epoch %d", freeze)
	}
	tipFreeze := freeze*V2RewardHalvingIntervalBlocks + 10
	wantFreeze := V2OrderBurnRate / math.Pow(2, float64(freeze))
	if got := OrderBurnRateAt(tipFreeze); math.Abs(got-wantFreeze) > 1e-15 {
		t.Fatalf("freeze burn got %v want %v (epoch %d)", got, wantFreeze, freeze)
	}
	tipPast := (freeze+3)*V2RewardHalvingIntervalBlocks + 10
	if got := OrderBurnRateAt(tipPast); math.Abs(got-wantFreeze) > 1e-15 {
		t.Fatalf("past-freeze burn must stay frozen: got %v want %v", got, wantFreeze)
	}
}

func TestEconomicsV2DigSoftStartGate(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX", "")
	ResetEconomicsV2ActivationHeightForTest()
	if DigSoftStartActiveAt(1_700_000_000) {
		t.Fatal("unset announce must not soft-start")
	}

	ann := int64(1_700_000_000)
	t.Setenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX", strconv.FormatInt(ann, 10))
	ResetEconomicsV2ActivationHeightForTest()
	if !DigSoftStartActiveAt(ann) {
		t.Fatal("at announce should soft-start")
	}
	if !DigSoftStartActiveAt(ann + 29*24*3600) {
		t.Fatal("day 29 should soft-start")
	}
	if DigSoftStartActiveAt(ann + 30*24*3600) {
		t.Fatal("day 30 boundary should end soft-start")
	}
	if DigSoftStartActiveAt(ann - 1) {
		t.Fatal("before announce must not soft-start")
	}
}

func TestOpenHuntEscrowBlockedDuringSoftStart(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	ann := time.Now().Unix() - 60
	t.Setenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX", strconv.FormatInt(ann, 10))
	ResetEconomicsV2ActivationHeightForTest()

	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "softstart.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	addr := "HMC-testnode"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 40.0)
	_, err = svc.OpenHuntEscrow(ctx, "hunt-soft-start", 20.0, 100)
	if !errors.Is(err, ErrHuntSoftStart) {
		t.Fatalf("want ErrHuntSoftStart, got %v", err)
	}
	// Dig 20/80 still allowed during soft-start.
	if _, err := svc.OpenFuzzEscrow(ctx, "dig-soft-start", 20.0, 100); err != nil {
		t.Fatalf("Dig escrow during soft-start: %v", err)
	}
}

// After v2 activation, empty mint no longer clamps at 100M — invariants must not
// brick the node if meta minted is above the legacy cap (long-run / import soak).
func TestEconomicsV2InvariantAllowsMintedAboveLegacyMax(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "1")
	ResetEconomicsV2ActivationHeightForTest()

	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain-overcap.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	addr := "HMC-testnode"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	over := HMCToUnits(MaxSupplyHMC) + HMCToUnits(1)
	if err := svc.upsertMetaUint(ctx, db, metaTotalMintedUnits, over); err != nil {
		t.Fatal(err)
	}
	if err := svc.upsertMetaFloat(ctx, db, metaTotalMintedHMC, UnitsToHMC(over)); err != nil {
		t.Fatal(err)
	}
	if err := svc.checkEconomicInvariants(ctx, db); err != nil {
		t.Fatalf("v2 active must allow minted>legacy max: %v", err)
	}

	ResetEconomicsV2ActivationHeightForTest()
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "")
	ResetEconomicsV2ActivationHeightForTest()
	if err := svc.checkEconomicInvariants(ctx, db); err == nil {
		t.Fatal("v1 schedule must still reject minted>max supply")
	}
}

func TestEconomicsV2AnnounceRejectsUint64Overflow(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX", "")
	ResetEconomicsV2ActivationHeightForTest()

	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "announce-overflow.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-testnode"); err != nil {
		t.Fatal(err)
	}
	overflow := uint64(math.MaxInt64) + 1
	if err := svc.upsertMetaUint(ctx, db, metaEconomicsV2AnnounceUnix, overflow); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	err = svc.lockEconomicsV2AnnounceTx(ctx, tx)
	_ = tx.Rollback()
	if err == nil {
		t.Fatal("expected overflow rejection for announce unix meta")
	}
}
