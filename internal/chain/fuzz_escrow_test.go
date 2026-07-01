package chain

import (
	"context"
	"path/filepath"
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
