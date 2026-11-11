package chain

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestHMSGenesisOneShotRefusesTreasuryRepin(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "hms-once.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	const first = "HMC-aaaaaaaaaaaaaaaa"
	const other = "HMC-bbbbbbbbbbbbbbbb"
	if err := svc.InitHMSGenesis(ctx, first); err != nil {
		t.Fatal(err)
	}
	ec1, err := svc.HMSEconomics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st1, err := svc.HmsAddressState(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE meta SET value='1' WHERE key=?`, metaHMSMaxSupplyUnits); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitHMSGenesis(ctx, other); !errors.Is(err, ErrGenesisAlreadyInitialized) {
		t.Fatalf("re-entry: %v", err)
	}
	ec2, err := svc.HMSEconomics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ec2.TreasuryAddress != first {
		t.Fatalf("treasury re-pinned to %q", ec2.TreasuryAddress)
	}
	if ec2.GenesisUnix != ec1.GenesisUnix || ec2.GenesisUnix == 0 {
		t.Fatalf("genesis marker changed: %d -> %d", ec1.GenesisUnix, ec2.GenesisUnix)
	}
	st2, err := svc.HmsAddressState(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	if st2.BalanceHMSUnits != st1.BalanceHMSUnits {
		t.Fatalf("float rewritten: %d -> %d", st1.BalanceHMSUnits, st2.BalanceHMSUnits)
	}
	var capRaw string
	if err := db.QueryRow(`SELECT value FROM meta WHERE key=?`, metaHMSMaxSupplyUnits).Scan(&capRaw); err != nil {
		t.Fatal(err)
	}
	if capRaw != "1" {
		t.Fatalf("cap re-pinned to %q", capRaw)
	}
	stOther, err := svc.HmsAddressState(ctx, other)
	if err != nil {
		t.Fatal(err)
	}
	if stOther.BalanceHMSUnits != 0 {
		t.Fatalf("second treasury credited %d", stOther.BalanceHMSUnits)
	}
}

func TestHMSGenesisFloatIsAdditive(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "hms-add.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	const treasury = "HMC-aaaaaaaaaaaaaaaa"
	const prior = uint64(50_000)
	if _, err := db.ExecContext(ctx,
		`INSERT INTO accounts (address, balance_units, balance_hms_units, next_nonce, hms_next_nonce, updated_at)
		 VALUES (?, 0, ?, 0, 0, strftime('%s','now'))`, treasury, prior); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitHMSGenesis(ctx, treasury); err != nil {
		t.Fatal(err)
	}
	st, err := svc.HmsAddressState(ctx, treasury)
	if err != nil {
		t.Fatal(err)
	}
	floatU := uint64(float64(HMSToUnits(MaxSupplyHMS)) * HMSTreasuryGenesisFloatPct)
	if st.BalanceHMSUnits != prior+floatU {
		t.Fatalf("float overwrite: got %d want %d (prior %d + float %d)", st.BalanceHMSUnits, prior+floatU, prior, floatU)
	}
}
