package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/fuzzescrow"
	"hackme/internal/store"
)

func TestCreditOrdersFundForOwnerIdempotentAndEscrow(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "prepaid.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	ctx := context.Background()
	node := "HMC-prepaidhubnode001"
	if _, _, err := svc.InitGenesis(ctx, node); err != nil {
		t.Fatal(err)
	}

	owner := "HMC-aabbccddeeff0011"
	units := HMCToUnits(20)
	acctUnits := func(addr string) uint64 {
		t.Helper()
		var u uint64
		if err := db.QueryRow(`SELECT COALESCE(balance_units,0) FROM accounts WHERE address=?`, addr).Scan(&u); err != nil {
			t.Fatalf("accounts %s: %v", addr, err)
		}
		return u
	}
	// Genesis parks treasury at DevFee; live Fund also credits DevFee via transfer.
	// Prepaid credit alone must not invent accounts units — open draws DevFee custody.
	devFunded := acctUnits(DevFeeAddress)
	if devFunded < units {
		t.Fatalf("treasury custody too low for test: %d < %d", devFunded, units)
	}

	r1, err := svc.CreditOrdersFundForOwner(ctx, owner, "ff00112233445566778899aabbccddee", units)
	if err != nil {
		t.Fatalf("credit: %v", err)
	}
	if r1.IdempotentReplay || r1.SpendableAfter < 19.9 {
		t.Fatalf("unexpected first credit: %+v", r1)
	}
	r2, err := svc.CreditOrdersFundForOwner(ctx, owner, "ff00112233445566778899aabbccddee", units)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !r2.IdempotentReplay {
		t.Fatalf("want idempotent replay, got %+v", r2)
	}
	bal, err := svc.GetOrdersPrepaidHMC(ctx, owner)
	if err != nil || bal < 19.9 {
		t.Fatalf("prepaid bal=%.4f err=%v", bal, err)
	}

	escrow, err := svc.OpenFuzzEscrowForOwner(ctx, "camp-tenant-1", owner, 20, 64, fuzzescrow.EscrowSplit2080, FeeAssetHMC)
	if err != nil {
		t.Fatalf("OpenFuzzEscrowForOwner: %v", err)
	}
	if OrdersPrepaidNormalizeAddr(escrow.PayerAddress) != OrdersPrepaidNormalizeAddr(owner) {
		t.Fatalf("payer_address=%q want %q", escrow.PayerAddress, owner)
	}
	after, err := svc.GetOrdersPrepaidHMC(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if after > 0.01 {
		t.Fatalf("prepaid should be spent on escrow, got %.4f", after)
	}
	devAfterOpen := acctUnits(DevFeeAddress)
	// Price 20 → ~10% fee; net DevFee drops by escrow+burn (platform re-credited).
	if devAfterOpen >= devFunded {
		t.Fatalf("DevFee custody must drop on tenant open: before=%d after=%d", devFunded, devAfterOpen)
	}

	canceled, err := svc.CancelFuzzEscrow(ctx, "camp-tenant-1")
	if err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if canceled.Status != "closed" {
		t.Fatalf("status=%s", canceled.Status)
	}
	refunded, err := svc.GetOrdersPrepaidHMC(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	// Economics v2 keeps ~10% fee; refund ≈ 0.9 of customer price.
	if refunded < 15.0 {
		t.Fatalf("expected prepaid refund after cancel, got %.4f (escrow=%+v)", refunded, canceled)
	}
	devAfterCancel := acctUnits(DevFeeAddress)
	// Refund restores escrow custody; fee burn stays out; platform stays at DevFee.
	if devAfterCancel <= devAfterOpen {
		t.Fatalf("DevFee custody must rise on tenant refund: open=%d cancel=%d", devAfterOpen, devAfterCancel)
	}
}

func TestOrdersFundOwnerFromMemo(t *testing.T) {
	addr := "HMC-aabbccddeeff0011"
	got := OrdersFundOwnerFromMemo(OrdersFundMemo(addr))
	if got != OrdersPrepaidNormalizeAddr(addr) {
		t.Fatalf("got %q", got)
	}
	if OrdersFundOwnerFromMemo("other") != "" {
		t.Fatal("expected empty")
	}
}

func TestTenantEscrowWhenPrimaryIsDevFeeKeepsWalletStable(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "devfee-primary.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	ctx := context.Background()
	if _, _, err := svc.InitGenesis(ctx, DevFeeAddress); err != nil {
		t.Fatal(err)
	}
	walletUnits := func() uint64 {
		t.Helper()
		var u uint64
		if err := db.QueryRow(`SELECT balance_units FROM wallet WHERE id=1`).Scan(&u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	acctUnits := func(addr string) uint64 {
		t.Helper()
		var u uint64
		if err := db.QueryRow(`SELECT COALESCE(balance_units,0) FROM accounts WHERE address=?`, addr).Scan(&u); err != nil {
			t.Fatal(err)
		}
		return u
	}
	w0 := walletUnits()
	owner := "HMC-ccddeeff00112233"
	units := HMCToUnits(20)
	if _, err := svc.CreditOrdersFundForOwner(ctx, owner, "aa00112233445566778899aabbccddee", units); err != nil {
		t.Fatal(err)
	}
	if walletUnits() != w0 {
		t.Fatalf("Fund prepaid must not move primary wallet when it is DevFee: before=%d after=%d", w0, walletUnits())
	}
	if _, err := svc.OpenFuzzEscrowForOwner(ctx, "camp-devfee-primary", owner, 20, 64, fuzzescrow.EscrowSplit2080, FeeAssetHMC); err != nil {
		t.Fatal(err)
	}
	if walletUnits() != w0 {
		t.Fatalf("tenant open must not move DevFee-primary wallet: before=%d after=%d", w0, walletUnits())
	}
	closed, err := svc.CancelFuzzEscrow(ctx, "camp-devfee-primary")
	if err != nil {
		t.Fatal(err)
	}
	if closed.RefundableHMC != 0 {
		t.Fatalf("closed escrow refundable_hmc=%v want 0", closed.RefundableHMC)
	}
	if walletUnits() != w0 {
		t.Fatalf("tenant cancel must not credit DevFee-primary wallet: before=%d after=%d acct=%d",
			w0, walletUnits(), acctUnits(DevFeeAddress))
	}
}

func TestClosedEscrowRefundableIsZero(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "refundable.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	ctx := context.Background()
	node := "HMC-refundablenode001"
	if _, _, err := svc.InitGenesis(ctx, node); err != nil {
		t.Fatal(err)
	}
	owner := "HMC-ddeeff0011223344"
	if _, err := svc.CreditOrdersFundForOwner(ctx, owner, "bb00112233445566778899aabbccddee", HMCToUnits(20)); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenFuzzEscrowForOwner(ctx, "camp-refundable", owner, 20, 64, fuzzescrow.EscrowSplit5050, FeeAssetHMC); err != nil {
		t.Fatal(err)
	}
	closed, err := svc.CancelFuzzEscrow(ctx, "camp-refundable")
	if err != nil {
		t.Fatal(err)
	}
	if closed.Status != "closed" || closed.RefundableHMC != 0 || closed.RunsRemainingHMC != 0 {
		t.Fatalf("want closed refundable=0 runs_remaining=0, got %+v", closed)
	}
	again, err := svc.GetFuzzEscrow(ctx, "camp-refundable")
	if err != nil {
		t.Fatal(err)
	}
	if again.RefundableHMC != 0 {
		t.Fatalf("GetFuzzEscrow refundable=%v", again.RefundableHMC)
	}
}
