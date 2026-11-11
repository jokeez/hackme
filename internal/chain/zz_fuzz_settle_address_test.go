package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestPayFuzzRunAcceptsLongAddress(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "settle-addr.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-1234567890123456"
	// Long address: HMC- + 64 lowercase hex.
	miner := "HMC-" + "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"
	if !IsValidAddress(miner) || !IsLongAddress(miner) {
		t.Fatalf("fixture miner must be long address: %s", miner)
	}
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 5)
	if _, err := svc.OpenFuzzEscrow(ctx, "long-addr", 1.0, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PayFuzzRun(ctx, "long-addr", miner); err != nil {
		t.Fatalf("PayFuzzRun must accept IsValidAddress long form: %v", err)
	}
	// Prefix-only / wrong length must still fail.
	if _, err := svc.PayFuzzRun(ctx, "long-addr", "HMC-deadbeef"); err == nil {
		t.Fatal("short/malformed miner must be rejected")
	}
}
