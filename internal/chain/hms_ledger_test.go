package chain

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/store"
)

func TestHMSGenesisMintTransferFeeSplit(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hms_test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{db: db}
	ctx := context.Background()

	treasury := "HMC-aaaaaaaaaaaaaaaa"
	if err := svc.InitHMSGenesis(ctx, treasury); err != nil {
		t.Fatalf("InitHMSGenesis: %v", err)
	}
	ec, err := svc.HMSEconomics(ctx)
	if err != nil {
		t.Fatalf("HMSEconomics: %v", err)
	}
	if ec.TreasuryAddress != treasury {
		t.Fatalf("treasury=%q want %q", ec.TreasuryAddress, treasury)
	}
	if ec.TotalMintedHMS <= 0 {
		t.Fatalf("expected genesis float minted, got %f", ec.TotalMintedHMS)
	}
	if !ec.Prelaunch || ec.LaneEnabled {
		t.Fatalf("default economics should be prelaunch (lane off): %+v", ec)
	}

	miner := "HMC-bbbbbbbbbbbbbbbb"
	mintU := HMSToUnits(10)
	if code, err := svc.MintHMS(ctx, miner, mintU, "test"); err != nil || code != "" {
		t.Fatalf("MintHMS: code=%s err=%v", code, err)
	}
	st, err := svc.HmsAddressState(ctx, miner)
	if err != nil || st.BalanceHMSUnits < mintU {
		t.Fatalf("miner balance: %+v err=%v", st, err)
	}
}

func TestHMSGenesisAcceptsLongTreasury(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "hms_long.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := &Service{db: db}
	ctx := context.Background()
	treasury := "HMC-" + strings.Repeat("ab", 32)
	if err := svc.InitHMSGenesis(ctx, treasury); err != nil {
		t.Fatalf("InitHMSGenesis long: %v", err)
	}
	ec, err := svc.HMSEconomics(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ec.TreasuryAddress != treasury {
		t.Fatalf("treasury=%q", ec.TreasuryAddress)
	}
	miner := "HMC-" + strings.Repeat("cd", 32)
	if code, err := svc.MintHMS(ctx, miner, HMSToUnits(1), "long-mint"); err != nil || code != "" {
		t.Fatalf("MintHMS long: code=%s err=%v", code, err)
	}
}

func TestValidateHMSTreasuryAddress(t *testing.T) {
	if err := validateHMSTreasuryAddress("HMC-aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	if err := validateHMSTreasuryAddress("HMC-" + strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if err := validateHMSTreasuryAddress("HMC-short"); err == nil {
		t.Fatal("want reject short")
	}
	if err := validateHMSTreasuryAddress("HMC-AAAAAAAAAAAAAAA"); err == nil {
		t.Fatal("want reject uppercase")
	}
}
