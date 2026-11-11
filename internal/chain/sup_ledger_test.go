package chain

import (
	"context"
	"errors"
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
	// Re-entry must refuse. It must not wipe minted or rewrite the cap.
	if _, err := db.Exec(`UPDATE meta SET value='1' WHERE key=?`, metaSUPMaxSupplyUnits); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitSUPGenesis(ctx); !errors.Is(err, ErrGenesisAlreadyInitialized) {
		t.Fatalf("genesis2: %v", err)
	}
	var capRaw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key=?`, metaSUPMaxSupplyUnits).Scan(&capRaw); err != nil {
		t.Fatal(err)
	}
	if capRaw != "1" {
		t.Fatalf("re-genesis re-pinned cap to %q", capRaw)
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

func TestSUPGenesisHealsMintedOnlyOnTrueGenesis(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "sup-heal.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	to := "HMC-suphealwallet01"
	pre := SUPToUnits(7.0)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, balance_sup_units, next_nonce, sup_next_nonce, updated_at)
		 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))`, to, pre); err != nil {
		t.Fatal(err)
	}
	// No genesis marker yet: the first call may raise minted to the balance sum.
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatalf("true genesis: %v", err)
	}
	ec, err := svc.SUPEconomics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ec.TotalMintedSUP < 6.99 {
		t.Fatalf("true-genesis heal failed: %+v", ec)
	}
	if _, err := db.ExecContext(ctx, `UPDATE accounts SET balance_sup_units=balance_sup_units+? WHERE address=?`, SUPToUnits(3.0), to); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value='0' WHERE key=?`, metaSUPTotalMintedUnits); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitSUPGenesis(ctx); !errors.Is(err, ErrGenesisAlreadyInitialized) {
		t.Fatalf("re-entry must refuse, got %v", err)
	}
	ec2, err := svc.SUPEconomics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ec2.TotalMintedSUP > 0.001 {
		t.Fatalf("re-entry upward-healed minted: %+v", ec2)
	}
}
