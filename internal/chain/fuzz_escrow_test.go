package chain

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/store"
)

func TestFuzzEscrow2080(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "fuzz.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	addr := "HMC-1234567890123456"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 20.0)
	row, err := svc.OpenFuzzEscrow(ctx, "camp-1", 10.0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if row.RunsPoolHMC < 1.99 || row.BountyPoolHMC < 7.99 {
		t.Fatalf("split: %+v", row)
	}
	miner := "HMC-9876543210987654"
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.PayFuzzRun(ctx, "camp-1", miner); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.PayFuzzBounty(ctx, "camp-1", miner, "high"); err != nil {
		t.Fatal(err)
	}
	final, err := svc.FinalizeFuzzEscrow(ctx, "camp-1")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "closed" {
		t.Fatalf("status %s", final.Status)
	}
}

func TestFuzzEscrowFinalizeRefundsUnpaidRuns(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "fuzz-fin-runs.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-1234567890123456"
	miner := "HMC-9876543210987654"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 20.0)
	var pre uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&pre); err != nil {
		t.Fatal(err)
	}
	opened, err := svc.OpenFuzzEscrow(ctx, "camp-fin-runs", 10.0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := svc.PayFuzzRun(ctx, "camp-fin-runs", miner); err != nil {
			t.Fatal(err)
		}
	}
	paid := opened.RunsPoolHMC * 3 / 100 // approximate; use row after pays
	rowMid, err := svc.GetFuzzEscrow(ctx, "camp-fin-runs")
	if err != nil {
		t.Fatal(err)
	}
	paid = rowMid.RunsPaidHMC
	final, err := svc.FinalizeFuzzEscrow(ctx, "camp-fin-runs")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "closed" {
		t.Fatalf("status %s", final.Status)
	}
	wantRunsRefund := opened.RunsPoolHMC - paid
	if final.RefundedRunsHMC < wantRunsRefund-1e-6 || final.RefundedRunsHMC > wantRunsRefund+1e-6 {
		t.Fatalf("refunded_runs=%v want ~%v (pool=%v paid=%v)", final.RefundedRunsHMC, wantRunsRefund, opened.RunsPoolHMC, paid)
	}
	if final.RefundedBountyHMC < opened.BountyPoolHMC-1e-6 {
		t.Fatalf("refunded_bounty=%v want full bounty pool %v", final.RefundedBountyHMC, opened.BountyPoolHMC)
	}
	var post uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&post); err != nil {
		t.Fatal(err)
	}
	// wallet should recover unpaid runs + full bounty (runs paid stayed with miner)
	if post <= pre-HMCToUnits(paid)-1000 {
		t.Fatalf("wallet not credited unpaid runs: pre=%d post=%d paid_runs_hmc=%v", pre, post, paid)
	}
}

func TestFuzzEscrowCancelRefundsUnpaid(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "fuzz-cancel.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	addr := "HMC-1234567890123456"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 20.0)
	var pre uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&pre); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenFuzzEscrow(ctx, "camp-cancel", 10.0, 100); err != nil {
		t.Fatal(err)
	}
	row, err := svc.CancelFuzzEscrow(ctx, "camp-cancel")
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != "closed" {
		t.Fatalf("status %s", row.Status)
	}
	var post uint64
	if err := db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&post); err != nil {
		t.Fatal(err)
	}
	if post < pre {
		t.Fatalf("cancel must refund escrow: pre=%d post=%d", pre, post)
	}
}

func TestFuzzEscrowCrashBonusAndLiveFields(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "fuzz-crash-bonus.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-1234567890123456"
	miner := "HMC-9876543210987654"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 20.0)
	opened, err := svc.OpenFuzzEscrow(ctx, "camp-crash", 10.0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if opened.SpentRunsHMC != 0 || opened.LockedBountyHMC < 7.99 {
		t.Fatalf("live fields on open: spent=%v locked=%v path=%s", opened.SpentRunsHMC, opened.LockedBountyHMC, opened.RefundPath)
	}
	if !strings.Contains(opened.RefundPath, "finalize_or_cancel") {
		t.Fatalf("refund_path=%q", opened.RefundPath)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PayFuzzRun(ctx, "camp-crash", miner); err != nil {
		t.Fatal(err)
	}
	bonus, err := svc.PayFuzzCrashBonus(ctx, "camp-crash", miner)
	if err != nil {
		t.Fatal(err)
	}
	if bonus.CrashBonusPaidHMC <= 0 || bonus.CrashBonusPaidHMC > 0.0100001 {
		t.Fatalf("crash bonus=%v", bonus.CrashBonusPaidHMC)
	}
	if bonus.Status != "open" {
		t.Fatalf("crash bonus must not close bounty, status=%s", bonus.Status)
	}
	if bonus.LockedBountyHMC >= opened.BountyPoolHMC {
		t.Fatalf("locked bounty should shrink after bonus: locked=%v pool=%v", bonus.LockedBountyHMC, opened.BountyPoolHMC)
	}
	if _, err := svc.PayFuzzCrashBonus(ctx, "camp-crash", miner); err == nil {
		t.Fatal("second crash bonus must fail")
	}
	bounty, err := svc.PayFuzzBounty(ctx, "camp-crash", miner, "critical")
	if err != nil {
		t.Fatal(err)
	}
	if bounty.Status != "bounty_paid" {
		t.Fatalf("status=%s", bounty.Status)
	}
	wantBounty := opened.BountyPoolHMC - bonus.CrashBonusPaidHMC
	if bounty.BountyPaidHMC < wantBounty-1e-6 || bounty.BountyPaidHMC > wantBounty+1e-6 {
		t.Fatalf("bounty_paid=%v want ~%v (pool - crash bonus)", bounty.BountyPaidHMC, wantBounty)
	}
	if bounty.LockedBountyHMC != 0 {
		t.Fatalf("locked after bounty=%v", bounty.LockedBountyHMC)
	}
	final, err := svc.FinalizeFuzzEscrow(ctx, "camp-crash")
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != "closed" || final.RefundPath != "already_closed" {
		t.Fatalf("final=%+v", final)
	}
}
