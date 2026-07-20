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

func TestSUPGenesisIdempotentPreservesMinted(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "sup-idem.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatalf("genesis1: %v", err)
	}
	ec1, _ := svc.SUPEconomics(ctx)
	genesis1 := ec1.GenesisUnix
	to := "HMC-supidempotent01"
	if code, err := svc.MintSUP(ctx, to, SUPToUnits(3.5), "idem"); err != nil || code != "" {
		t.Fatalf("MintSUP: code=%q err=%v", code, err)
	}
	// Legacy bug: repeated genesis must NOT wipe total_minted.
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatalf("genesis2: %v", err)
	}
	ec2, err := svc.SUPEconomics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ec2.GenesisUnix != genesis1 {
		t.Fatalf("genesis_unix rewritten: %d -> %d", genesis1, ec2.GenesisUnix)
	}
	if ec2.TotalMintedSUP < 3.49 {
		t.Fatalf("total_minted wiped by re-genesis: %+v", ec2)
	}
}

func TestSUPGenesisSelfHealsMintedFloor(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "sup-heal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatalf("genesis: %v", err)
	}
	to := "HMC-suphealwallet01"
	if code, err := svc.MintSUP(ctx, to, SUPToUnits(7.0), "heal"); err != nil || code != "" {
		t.Fatalf("MintSUP: %v %s", err, code)
	}
	// Simulate legacy wipe of minted counter while balances remain.
	if _, err := db.Exec(`UPDATE meta SET value='0' WHERE key=?`, metaSUPTotalMintedUnits); err != nil {
		t.Fatal(err)
	}
	ecBroken, _ := svc.SUPEconomics(ctx)
	if ecBroken.TotalMintedSUP > 0.001 {
		t.Fatalf("setup failed, minted still %v", ecBroken.TotalMintedSUP)
	}
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatalf("heal genesis: %v", err)
	}
	ec, err := svc.SUPEconomics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ec.TotalMintedSUP < 6.99 {
		t.Fatalf("self-heal failed: economics=%+v", ec)
	}
}
