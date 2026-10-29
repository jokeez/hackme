package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestPayFuzzRunFloorIsFractional(t *testing.T) {
	t.Setenv("HACKME_HUNT_ENGINE_FLOOR_PAY_BPS", "1000") // 10%
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "floor.db"))
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
	camp := "hunt-floor-frac"
	if _, err := svc.OpenFuzzEscrowSplit(ctx, camp, 20, 8, "50_50"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}

	var perRunUnits uint64
	if err := db.QueryRowContext(ctx, `SELECT per_run_units FROM fuzz_campaign_escrow WHERE campaign_id=?`, camp).Scan(&perRunUnits); err != nil {
		t.Fatal(err)
	}
	if perRunUnits == 0 {
		t.Fatal("per_run_units=0")
	}

	row, newly, err := svc.ApplyFuzzSettleOnce(ctx, "evt-floor-1", "run_floor", camp, miner, "")
	if err != nil {
		t.Fatal(err)
	}
	if !newly || row == nil {
		t.Fatalf("want newly applied floor, got newly=%v row=%v", newly, row)
	}
	full, err := svc.GetFuzzEscrow(ctx, camp)
	if err != nil || full == nil {
		t.Fatal(err)
	}
	if full.RunsDone != 1 {
		t.Fatalf("runs_done=%d want 1", full.RunsDone)
	}

	var paidUnits uint64
	if err := db.QueryRowContext(ctx, `SELECT runs_paid_units FROM fuzz_campaign_escrow WHERE campaign_id=?`, camp).Scan(&paidUnits); err != nil {
		t.Fatal(err)
	}
	if paidUnits == 0 || paidUnits >= perRunUnits {
		t.Fatalf("floor paid %d; want 0 < paid < per_run %d", paidUnits, perRunUnits)
	}
	want := perRunUnits / 10
	if perRunUnits >= 10 && paidUnits != want {
		t.Fatalf("paid %d want %d (10%% of %d)", paidUnits, want, perRunUnits)
	}

	// Full run still pays remaining per-run (not floor).
	row2, newly2, err := svc.ApplyFuzzSettleOnce(ctx, "evt-full-2", "run", camp, miner, "")
	if err != nil || !newly2 {
		t.Fatalf("full run: newly=%v err=%v", newly2, err)
	}
	if row2.RunsDone != 2 {
		t.Fatalf("runs_done=%d want 2", row2.RunsDone)
	}
	var paid2 uint64
	if err := db.QueryRowContext(ctx, `SELECT runs_paid_units FROM fuzz_campaign_escrow WHERE campaign_id=?`, camp).Scan(&paid2); err != nil {
		t.Fatal(err)
	}
	paidDelta := paid2 - paidUnits
	if paidDelta != perRunUnits {
		t.Fatalf("full run delta paid %d want per_run %d", paidDelta, perRunUnits)
	}
}
