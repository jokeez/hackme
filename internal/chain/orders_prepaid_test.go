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
