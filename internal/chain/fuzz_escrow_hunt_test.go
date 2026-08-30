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
