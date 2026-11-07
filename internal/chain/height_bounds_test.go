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
}
