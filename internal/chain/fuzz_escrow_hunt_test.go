package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/fuzzescrow"
	"hackme/internal/store"
)

func TestOpenHuntEscrow5050(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "hunt.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	addr := "HMC-1234567890123456"
	if _, _, err := svc.InitGenesis(ctx, addr); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, addr, 30.0)
	row, err := svc.OpenHuntEscrow(ctx, "hunt-escrow-1", 20.0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if row.RunsPoolHMC < 9.9 || row.RunsPoolHMC > 10.1 {
		t.Fatalf("runs pool=%v", row.RunsPoolHMC)
	}
	if row.BountyPoolHMC < 9.9 || row.BountyPoolHMC > 10.1 {
		t.Fatalf("bounty pool=%v", row.BountyPoolHMC)
	}
	var split string
	if err := db.QueryRow(`SELECT escrow_split FROM fuzz_campaign_escrow WHERE campaign_id=?`, "hunt-escrow-1").Scan(&split); err != nil {
		t.Fatal(err)
	}
	if split != fuzzescrow.EscrowSplit5050 {
		t.Fatalf("escrow_split=%q", split)
	}
}

func TestPayHuntBountyHighPays60Percent(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "hunt-bounty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-1234567890123456"
	miner := "HMC-9876543210987654"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 30.0)
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	row, err := svc.OpenHuntEscrow(ctx, "hunt-bounty-high", 20.0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	bountyPool := uint64(row.BountyPoolHMC * 100_000_000)
	if _, err := svc.PayFuzzBounty(ctx, "hunt-bounty-high", miner, "high"); err != nil {
		t.Fatal(err)
	}
	var minerBal int64
	if err := db.QueryRow(`SELECT balance_units FROM accounts WHERE address=?`, miner).Scan(&minerBal); err != nil {
		t.Fatal(err)
	}
	var paidUnits uint64
	if err := db.QueryRow(`SELECT bounty_paid_units FROM fuzz_campaign_escrow WHERE campaign_id=?`, "hunt-bounty-high").Scan(&paidUnits); err != nil {
		t.Fatal(err)
	}
	wantSlice := uint64(float64(bountyPool) * 0.6)
	if paidUnits < wantSlice-2 || paidUnits > wantSlice+2 {
		t.Fatalf("paid_units=%d want ~%d", paidUnits, wantSlice)
	}
	if minerBal <= 0 {
		t.Fatalf("miner balance=%d", minerBal)
	}
}
