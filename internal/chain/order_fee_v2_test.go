package chain

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"hackme/internal/fuzzescrow"
	"hackme/internal/store"
)

type feeTestChain struct {
	svc    *Service
	db     *sql.DB
	ctx    context.Context
	wallet string
}

func newFeeTestChain(t *testing.T, activation string, hmc float64, supUnits uint64) feeTestChain {
	t.Helper()
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", activation)
	t.Setenv("HACKME_ECONOMICS_V2_ANNOUNCE_UNIX", "")
	ResetEconomicsV2ActivationHeightForTest()
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "fee.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	wallet := "HMC-1234567890123456"
	if _, _, err := svc.InitGenesis(ctx, wallet); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, wallet, hmc)
	if _, err := db.Exec(`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, metaTotalMintedUnits, HMCToUnits(hmc)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE accounts SET balance_sup_units=? WHERE address=?`, supUnits, wallet); err != nil {
		t.Fatal(err)
	}
	return feeTestChain{svc: svc, db: db, ctx: ctx, wallet: wallet}
}

func (c feeTestChain) balances(t *testing.T) (hmcUnits, supUnits, devUnits, burnedUnits uint64) {
	t.Helper()
	if err := c.db.QueryRow(`SELECT balance_units, COALESCE(balance_sup_units,0) FROM accounts WHERE address=?`, c.wallet).Scan(&hmcUnits, &supUnits); err != nil {
		t.Fatal(err)
	}
	_ = c.db.QueryRow(`SELECT balance_units FROM accounts WHERE address=?`, DevFeeAddress).Scan(&devUnits)
	_, burnedUnits, _ = c.svc.econTotalsUnits(c.ctx, c.db)
	return
}

func TestOrderFeeV2Split(t *testing.T) {
	ResetEconomicsV2ActivationHeightForTest()
	t.Cleanup(ResetEconomicsV2ActivationHeightForTest)
	t.Setenv("HACKME_ECONOMICS_V2_ACTIVATION_HEIGHT", "1")
	ResetEconomicsV2ActivationHeightForTest()
	f, escrow := OrderFeeV2FromPrice(10, HMCToUnits(60), FeeAssetHMC)
	if escrow != HMCToUnits(54) || f.BurnUnits != HMCToUnits(3) || f.PlatformUnits != HMCToUnits(3) || f.SUPUnits != 0 {
		t.Fatalf("Hunt Standard 60: escrow=%d fee=%+v", escrow, f)
	}
	s, escrowS := OrderFeeV2FromPrice(10, HMCToUnits(60), FeeAssetSUP)
	if escrowS != escrow || s.SUPUnits != HMCToUnits(6) || s.HMCUnits() != 0 {
		t.Fatalf("SUP fee: escrow=%d fee=%+v", escrowS, s)
	}
	// PoH order: escrow fixed, price = escrow/0.9 ⇒ fee ≈ escrow/9.
	p := OrderFeeV2ForEscrow(10, HMCToUnits(9), FeeAssetHMC)
	if p.BurnUnits != HMCToUnits(0.5) || p.PlatformUnits != HMCToUnits(0.5) {
		t.Fatalf("PoH fee for 9 HMC escrow: %+v", p)
	}
	// Era 1: burn halves, platform stays.
	e1, _ := OrderFeeV2FromPrice(V2RewardHalvingIntervalBlocks, HMCToUnits(100), FeeAssetHMC)
	if e1.BurnUnits != HMCToUnits(2.5) || e1.PlatformUnits != HMCToUnits(5) {
		t.Fatalf("era 1: %+v", e1)
	}
	if _, err := NormalizeFeeAsset("btc"); !errors.Is(err, ErrFeeAsset) {
		t.Fatal("unknown fee asset must fail")
	}
}

// RFC C5: escrow is the same whichever asset pays the fee; burn is real; platform gets its leg.
func TestDigEscrowV2FeeHMCAndSUP(t *testing.T) {
	c := newFeeTestChain(t, "1", 200, HMCToUnits(20))
	h0, s0, d0, b0 := c.balances(t)
	row, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "dig-hmc", 100, 100, fuzzescrow.EscrowSplit2080, "HMC")
	if err != nil {
		t.Fatal(err)
	}
	h1, s1, d1, b1 := c.balances(t)
	if h0-h1 != HMCToUnits(100) || s1 != s0 || d1-d0 != HMCToUnits(5) || b1-b0 != HMCToUnits(5) {
		t.Fatalf("HMC fee: wallet -%d sup %d->%d dev +%d burned +%d", h0-h1, s0, s1, d1-d0, b1-b0)
	}
	if HMCToUnits(row.RunsPoolHMC+row.BountyPoolHMC) != HMCToUnits(90) {
		t.Fatalf("escrow %v+%v want 90", row.RunsPoolHMC, row.BountyPoolHMC)
	}

	rowS, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "dig-sup", 100, 100, fuzzescrow.EscrowSplit2080, "sup")
	if err != nil {
		t.Fatal(err)
	}
	h2, s2, d2, b2 := c.balances(t)
	if h1-h2 != HMCToUnits(90) || s1-s2 != HMCToUnits(10) || d2 != d1 || b2 != b1 {
		t.Fatalf("SUP fee: wallet HMC -%d SUP -%d dev %d->%d burned %d->%d", h1-h2, s1-s2, d1, d2, b1, b2)
	}
	if rowS.RunsPoolHMC != row.RunsPoolHMC || rowS.BountyPoolHMC != row.BountyPoolHMC {
		t.Fatalf("escrow differs by fee asset: %+v vs %+v", rowS, row)
	}
	var asset string
	var supFee uint64
	if err := c.db.QueryRow(`SELECT fee_asset, fee_sup_units FROM fuzz_campaign_escrow WHERE campaign_id='dig-sup'`).Scan(&asset, &supFee); err != nil || asset != "SUP" || supFee != HMCToUnits(10) {
		t.Fatalf("fee record asset=%q sup=%d err=%v", asset, supFee, err)
	}
	ec, _ := c.svc.SUPEconomics(c.ctx)
	if ec.TotalBurnedSUP != 10 {
		t.Fatalf("SUP burned %v want 10", ec.TotalBurnedSUP)
	}
}

func TestDigEscrowV2SUPFeeShortfallChargesNothing(t *testing.T) {
	c := newFeeTestChain(t, "1", 200, HMCToUnits(5))
	h0, s0, d0, b0 := c.balances(t)
	if _, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "dig-short", 100, 100, fuzzescrow.EscrowSplit2080, "SUP"); !errors.Is(err, ErrSUPFeeInsufficent) {
		t.Fatalf("want ErrSUPFeeInsufficent, got %v", err)
	}
	h1, s1, d1, b1 := c.balances(t)
	if h1 != h0 || s1 != s0 || d1 != d0 || b1 != b0 {
		t.Fatal("failed open must not move any balance")
	}
	var n int
	_ = c.db.QueryRow(`SELECT COUNT(*) FROM fuzz_campaign_escrow`).Scan(&n)
	if n != 0 {
		t.Fatal("failed open left an escrow row")
	}
}

func TestDigEscrowPreV2Unchanged(t *testing.T) {
	c := newFeeTestChain(t, "", 200, HMCToUnits(20))
	h0, _, _, b0 := c.balances(t)
	row, err := c.svc.OpenFuzzEscrowSplit(c.ctx, "dig-v1", 100, 100, fuzzescrow.EscrowSplit2080)
	if err != nil {
		t.Fatal(err)
	}
	h1, _, _, b1 := c.balances(t)
	if h0-h1 != HMCToUnits(100) || b1 != b0 || HMCToUnits(row.RunsPoolHMC+row.BountyPoolHMC) != HMCToUnits(100) {
		t.Fatalf("v1 escrow changed: debit=%d burned %d->%d row=%+v", h0-h1, b0, b1, row)
	}
	if _, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "dig-v1-sup", 100, 100, fuzzescrow.EscrowSplit2080, "SUP"); err == nil {
		t.Fatal("SUP fee before v2 must be refused")
	}
}

func TestPoHOrderV2FeeRealBurn(t *testing.T) {
	c := newFeeTestChain(t, "1", 50, HMCToUnits(5))
	h0, s0, d0, b0 := c.balances(t)
	manifest, _ := json.Marshal(map[string]any{
		"id": "ord-v2-hmc", "kind": "synthetic_poh_v1", "artifact_hash": "ab",
		"reward_hmc": 0.9, "target_solves": 10, "timeout_ms": 5000, "payer_ref": "t",
	})
	res, err := c.svc.InsertOrderTask(c.ctx, manifest)
	if err != nil {
		t.Fatal(err)
	}
	h1, s1, d1, b1 := c.balances(t)
	// escrow 9 HMC, fee 1 HMC (0.5 burn + 0.5 platform) ⇒ wallet -10.
	if h0-h1 != HMCToUnits(10) || d1-d0 != HMCToUnits(0.5) || b1-b0 != HMCToUnits(0.5) || s1 != s0 {
		t.Fatalf("v2 HMC order: wallet -%d dev +%d burned +%d", h0-h1, d1-d0, b1-b0)
	}
	if res.Fee == nil || res.Fee.Asset != FeeAssetHMC || res.PrepaidHMC != 9 {
		t.Fatalf("result %+v", res)
	}
	manifestS, _ := json.Marshal(map[string]any{
		"id": "ord-v2-sup", "kind": "synthetic_poh_v1", "artifact_hash": "ab",
		"reward_hmc": 0.9, "target_solves": 10, "timeout_ms": 5000, "payer_ref": "t", "fee_asset": "SUP",
	})
	if _, err := c.svc.InsertOrderTask(c.ctx, manifestS); err != nil {
		t.Fatal(err)
	}
	h2, s2, d2, b2 := c.balances(t)
	if h1-h2 != HMCToUnits(9) || s1-s2 != HMCToUnits(1) || d2 != d1 || b2 != b1 {
		t.Fatalf("v2 SUP order: HMC -%d SUP -%d dev %d->%d", h1-h2, s1-s2, d1, d2)
	}
}
