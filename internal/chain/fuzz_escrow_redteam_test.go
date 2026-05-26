package chain

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestFuzzEscrowRedteamDoubleBounty(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-1111111111111111"
	miner := "HMC-2222222222222222"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 50)
	if _, err := svc.OpenFuzzEscrow(ctx, "rt-bounty", 5.0, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address,balance_units,next_nonce,updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PayFuzzBounty(ctx, "rt-bounty", miner, "high"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.PayFuzzBounty(ctx, "rt-bounty", miner, "critical")
	if !errors.Is(err, ErrFuzzEscrowAlreadyPaid) && !errors.Is(err, ErrFuzzEscrowClosed) {
		t.Fatalf("want already paid/closed, got %v", err)
	}
}

func TestFuzzEscrowRedteamDrainRunsPool(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-aaaaaaaaaaaaaaaa"
	miner := "HMC-bbbbbbbbbbbbbbbb"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 100)
	if _, err := svc.OpenFuzzEscrow(ctx, "rt-runs", 2.0, 20); err != nil {
		t.Fatal(err)
	}
	var runsPaid float64
	for i := 0; i < 500; i++ {
		row, err := svc.PayFuzzRun(ctx, "rt-runs", miner)
		if errors.Is(err, ErrFuzzEscrowDepleted) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		runsPaid = row.RunsPaidHMC
	}
	if runsPaid > 0.41 {
		t.Fatalf("runs pool drained beyond 20%% cap: paid %f HMC", runsPaid)
	}
}

func TestFuzzEscrowRedteamInvalidMiner(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-cccccccccccccccc"); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, "HMC-cccccccccccccccc", 5)
	if _, err := svc.OpenFuzzEscrow(ctx, "rt-inv", 1.0, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PayFuzzRun(ctx, "rt-inv", "not-an-address"); err == nil {
		t.Fatal("expected invalid miner rejection")
	}
}

func TestFuzzEscrowRedteamInsufficientOpen(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-dddddddddddddddd"); err != nil {
		t.Fatal(err)
	}
	_, err = svc.OpenFuzzEscrow(ctx, "rt-poor", 100.0, 50)
	if !errors.Is(err, ErrFuzzInsufficientBalance) {
		t.Fatalf("want insufficient balance, got %v", err)
	}
}

func TestFuzzEscrowRedteamFinalizeIdempotent(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "rt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-eeeeeeeeeeeeeeee"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 10)
	if _, err := svc.OpenFuzzEscrow(ctx, "rt-fin", 3.0, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FinalizeFuzzEscrow(ctx, "rt-fin"); err != nil {
		t.Fatal(err)
	}
	row, err := svc.FinalizeFuzzEscrow(ctx, "rt-fin")
	if err != nil || row.Status != "closed" {
		t.Fatalf("finalize idempotent: %v %+v", err, row)
	}
	_, err = svc.PayFuzzRun(ctx, "rt-fin", "HMC-ffffffffffffffff")
	if !errors.Is(err, ErrFuzzEscrowClosed) {
		t.Fatalf("pay after close: %v", err)
	}
}
