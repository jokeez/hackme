package chain

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"hackme/internal/block"
	"hackme/internal/store"
)

func TestMintSUPIdempotentByMemo(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "sup-mint-idem.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatal(err)
	}
	to := "HMC-cccccccccccccc01"
	units := SUPToUnits(2.5)
	memo := "worker_sup_settlement:w1"
	if code, err := svc.MintSUP(ctx, to, units, memo); err != nil || code != "" {
		t.Fatalf("mint1: code=%q err=%v", code, err)
	}
	if code, err := svc.MintSUP(ctx, to, units, memo); err != nil || code != "" {
		t.Fatalf("mint2 replay: code=%q err=%v", code, err)
	}
	st, err := svc.SupAddressState(ctx, to)
	if err != nil {
		t.Fatal(err)
	}
	if st.BalanceSUPUnits != units {
		t.Fatalf("double mint: bal=%d want %d", st.BalanceSUPUnits, units)
	}
	// Different amount with same memo must mint again.
	extra := SUPToUnits(1.0)
	if code, err := svc.MintSUP(ctx, to, extra, memo); err != nil || code != "" {
		t.Fatalf("mint3 different amount: code=%q err=%v", code, err)
	}
	st2, _ := svc.SupAddressState(ctx, to)
	if st2.BalanceSUPUnits != units+extra {
		t.Fatalf("after third mint: bal=%d", st2.BalanceSUPUnits)
	}
}

func TestMintHMSIdempotentByMemo(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "hms-mint-idem.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if err := svc.InitHMSGenesis(ctx, "HMC-aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	to := "HMC-bbbbbbbbbbbbbbbb"
	units := HMSToUnits(4.0)
	memo := "hms_epoch_settle:e1:w1:miner"
	if code, err := svc.MintHMS(ctx, to, units, memo); err != nil || code != "" {
		t.Fatalf("mint1: code=%q err=%v", code, err)
	}
	if code, err := svc.MintHMS(ctx, to, units, memo); err != nil || code != "" {
		t.Fatalf("mint2 replay: code=%q err=%v", code, err)
	}
	st, err := svc.HmsAddressState(ctx, to)
	if err != nil {
		t.Fatal(err)
	}
	if st.BalanceHMSUnits != units {
		t.Fatalf("double mint: bal=%d want %d", st.BalanceHMSUnits, units)
	}
}

func TestOrderPayoutDualLedgerPrimaryWallet(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "ord-dual.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	addr := "HMC-orderdual000001"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 20.0)
	manifest, _ := json.Marshal(map[string]any{
		"id":            "ord-dual-1",
		"kind":          "synthetic_poh_v1",
		"artifact_hash": "ab",
		"reward_hmc":    0.05,
		"target_solves": 1,
		"payer_ref":     "test",
	})
	if _, err := svc.InsertOrderTask(ctx, manifest); err != nil {
		t.Fatal(err)
	}
	var walletBefore, acctBefore uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&walletBefore); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address=?`, addr).Scan(&acctBefore); err != nil {
		t.Fatal(err)
	}
	if walletBefore != acctBefore {
		t.Fatalf("precondition dual-ledger drift: wallet=%d accounts=%d", walletBefore, acctBefore)
	}
	m, _ := svc.PoHTargetMod(ctx)
	n, ev := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, addr, n, ev, 0.05, m, "ord-dual-1"); err != nil {
		t.Fatal(err)
	}
	var walletAfter, acctAfter uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&walletAfter); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM accounts WHERE address=?`, addr).Scan(&acctAfter); err != nil {
		t.Fatal(err)
	}
	if walletAfter != acctAfter {
		t.Fatalf("order payout broke dual-ledger: wallet=%d accounts=%d", walletAfter, acctAfter)
	}
	wantCredit := HMCToUnits(0.05)
	if walletAfter != walletBefore+wantCredit {
		t.Fatalf("wallet credit: before=%d after=%d want +%d", walletBefore, walletAfter, wantCredit)
	}
}

func TestImportPoHBlockRejectsOrderEscrowByDefault(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "import-ord.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	addr := "HMC-importord000001"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	m, _ := svc.PoHTargetMod(ctx)
	n, ev := firstPoHHit(m)
	h, tip, _ := svc.Tip(ctx)
	b := block.NewPoHBlock(h+1, tip, addr, n, ev, m, "ord-imported", PoHFormulaLabelForIndex(h+1))
	err = svc.ImportPoHBlock(ctx, b)
	if !errors.Is(err, ErrImportOrderEscrowDenied) {
		t.Fatalf("want ErrImportOrderEscrowDenied, got %v", err)
	}
}

func TestImportPoHBlockCreditsBaseReward(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "import-base.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	addr := "HMC-importbase00001"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	var walletBefore uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&walletBefore); err != nil {
		t.Fatal(err)
	}
	m, _ := svc.PoHTargetMod(ctx)
	n, ev := firstPoHHit(m)
	h, tip, _ := svc.Tip(ctx)
	b := block.NewPoHBlock(h+1, tip, addr, n, ev, m, "", PoHFormulaLabelForIndex(h+1))
	if err := svc.ImportPoHBlock(ctx, b); err != nil {
		t.Fatal(err)
	}
	h2, tip2, _ := svc.Tip(ctx)
	if h2 != h+1 || tip2 != b.Hash {
		t.Fatalf("tip not advanced: h=%d tip=%s", h2, tip2)
	}
	var walletAfter uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&walletAfter); err != nil {
		t.Fatal(err)
	}
	want := HMCToUnits(BaseRewardForBlockIndex(h + 1))
	if walletAfter != walletBefore+want {
		t.Fatalf("wallet: before=%d after=%d want +%d", walletBefore, walletAfter, want)
	}
}
