package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestSUPGenesisMint(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "sup.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatalf("InitSUPGenesis: %v", err)
	}
	to := "HMC-suptestwallet01"
	if code, err := svc.MintSUP(ctx, to, SUPToUnits(10.0), "test_mint"); err != nil || code != "" {
		t.Fatalf("MintSUP: code=%q err=%v", code, err)
	}
	st, err := svc.SupAddressState(ctx, to)
	if err != nil {
		t.Fatalf("SupAddressState: %v", err)
	}
	if st.BalanceSUP < 9.99 {
		t.Fatalf("balance sup want ~10 got %v", st.BalanceSUP)
	}
	ec, err := svc.SUPEconomics(ctx)
	if err != nil {
		t.Fatalf("SUPEconomics: %v", err)
	}
	if !ec.MintEnabled || ec.TotalMintedSUP < 9.99 {
		t.Fatalf("economics: %+v", ec)
	}
}
