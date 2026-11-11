package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestOrdersEscrowLockedUnitsIncludesOpenHunt(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "escrow-locked.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	addr := "HMC-1234567890123456"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 40.0)

	before, err := svc.OrdersEscrowLockedUnits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenHuntEscrow(ctx, "lock-hunt-1", 20.0, 100); err != nil {
		t.Fatal(err)
	}
	after, err := svc.OrdersEscrowLockedUnits(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if after <= before {
		t.Fatalf("locked units did not rise: before=%d after=%d", before, after)
	}
	hmc, units, err := svc.OrdersEscrowLockedHMC(ctx)
	if err != nil || units != after || hmc <= 0 {
		t.Fatalf("hmc=%v units=%d err=%v", hmc, units, err)
	}
}
