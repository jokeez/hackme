package chain

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestFitsSQLiteBlockHeightBounds(t *testing.T) {
	if !FitsSQLiteBlockHeight(0) || !FitsSQLiteBlockHeight(math.MaxInt64) {
		t.Fatal("0 and MaxInt64 must fit")
	}
	if FitsSQLiteBlockHeight(uint64(math.MaxInt64) + 1) {
		t.Fatal("2^63 must not fit")
	}
	if FitsSQLiteBlockHeight(^uint64(0)) {
		t.Fatal("MaxUint64 must not fit")
	}
	if err := CheckSQLiteBlockHeight(uint64(math.MaxInt64) + 1); !errors.Is(err, ErrHeightOutOfRange) {
		t.Fatalf("want ErrHeightOutOfRange, got %v", err)
	}
}

func TestListBlocksFromHeightRejectsHighBitUint64(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	ctx := context.Background()
	if _, err := svc.ListBlocksFromHeight(ctx, uint64(math.MaxInt64)+1, 1); !errors.Is(err, ErrHeightOutOfRange) {
		t.Fatalf("from_height 2^63: %v", err)
	}
	if _, err := svc.ListBlocksFromHeight(ctx, math.MaxInt64, 1); err != nil {
		t.Fatalf("from_height MaxInt64 should query ok (empty): %v", err)
	}
	if _, err := svc.ListBlockSummariesBefore(ctx, uint64(math.MaxInt64)+1, 1); !errors.Is(err, ErrHeightOutOfRange) {
		t.Fatalf("before 2^63: %v", err)
	}
	if _, err := svc.ListBlockSummariesBefore(ctx, math.MaxInt64, 1); err != nil {
		t.Fatalf("before MaxInt64 should query ok: %v", err)
	}
	if _, _, err := svc.GetBlockSummaryByIndex(ctx, uint64(math.MaxInt64)+1); !errors.Is(err, ErrHeightOutOfRange) {
		t.Fatalf("index 2^63: %v", err)
	}
}

func TestTransfersByFromNonceRejectsHighBitUint64(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "nonce.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	ctx := context.Background()
	if _, err := svc.TransfersByFromNonce(ctx, "HMC-1111111111111111", uint64(math.MaxInt64)+1); !errors.Is(err, ErrHeightOutOfRange) {
		t.Fatalf("nonce 2^63: %v", err)
	}
	if _, err := svc.TransfersByFromNonce(ctx, "HMC-1111111111111111", math.MaxInt64); err != nil {
		t.Fatalf("nonce MaxInt64 should query ok: %v", err)
	}
}

func TestTransferShapesRejectSQLiteHighBitUint64(t *testing.T) {
	hi := uint64(math.MaxInt64) + 1
	okFrom, okTo := "HMC-aaaaaaaaaaaaaaaa", "HMC-bbbbbbbbbbbbbbbb"
	if code, _ := ValidateTransferShape(TransferTx{
		TxType: "transfer_v1", From: okFrom, To: okTo, AmountUnits: hi, FeeUnits: DefaultTransferMinFee,
		Nonce: 0, TimestampUnix: 1,
	}); code == "" {
		t.Fatal("HMC shape must reject high-bit amount")
	}
	if code, _ := ValidateTransferShape(TransferTx{
		TxType: "transfer_v1", From: okFrom, To: okTo, AmountUnits: 1, FeeUnits: DefaultTransferMinFee,
		Nonce: hi, TimestampUnix: 1,
	}); code == "" {
		t.Fatal("HMC shape must reject high-bit nonce")
	}
	if code, _ := ValidateSupTransferShape(SupTransferTx{
		TxType: "transfer_sup_v1", From: okFrom, To: okTo, AmountUnits: 1, FeeUnits: DefaultSUPTransferMinFee, Nonce: hi,
	}); code == "" {
		t.Fatal("SUP shape must reject high-bit nonce")
	}
	if code, _ := ValidateHmsTransferShape(HmsTransferTx{
		TxType: "transfer_hms_v1", From: okFrom, To: okTo, AmountUnits: 1, FeeUnits: DefaultHMSTransferMinFee, Nonce: hi,
	}); code == "" {
		t.Fatal("HMS shape must reject high-bit nonce")
	}
	if code, err := validateSUPFeeShape(okFrom, hi, "api", "ref-1"); code == "" || err == nil {
		t.Fatal("SUP fee shape must reject high-bit amount")
	}
}

func TestMintRejectsSQLiteHighBitAmount(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "mint.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	ctx := context.Background()
	hi := uint64(math.MaxInt64) + 1
	if code, err := svc.MintSUP(ctx, "HMC-1111111111111111", hi, "audit"); code != "invalid_amount" || err == nil {
		t.Fatalf("MintSUP: code=%q err=%v", code, err)
	}
	if code, err := svc.MintHMS(ctx, "HMC-1111111111111111", hi, "audit"); code != "invalid_amount" || err == nil {
		t.Fatalf("MintHMS: code=%q err=%v", code, err)
	}
}
