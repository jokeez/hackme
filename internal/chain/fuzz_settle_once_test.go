package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestApplyFuzzSettleOnceNoDoublePay(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "once.db"))
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
	if _, err := svc.OpenFuzzEscrow(ctx, "once-camp", 10.0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}

	eventID := FuzzSettleEventID(99)
	row, newly, err := svc.ApplyFuzzSettleOnce(ctx, eventID, "run", "once-camp", miner, "")
	if err != nil || !newly {
		t.Fatalf("first apply: newly=%v err=%v", newly, err)
	}
	if row == nil || row.RunsDone != 1 {
		t.Fatalf("runs_done=%v want 1", row)
	}
	row, newly, err = svc.ApplyFuzzSettleOnce(ctx, eventID, "run", "once-camp", miner, "")
	if err != nil || newly {
		t.Fatalf("second apply must be no-op: newly=%v err=%v", newly, err)
	}
	if row.RunsDone != 1 {
		t.Fatalf("double-pay: runs_done=%d", row.RunsDone)
	}
}

func TestApplyFuzzSettleOnceTransientDoesNotMark(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "fail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	payer := "HMC-1111111111111111"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 50)
	if _, err := svc.OpenFuzzEscrow(ctx, "fail-camp", 10.0, 100); err != nil {
		t.Fatal(err)
	}
	// Invalid miner → pay fails; event must not stick (retry can succeed later).
	_, newly, err := svc.ApplyFuzzSettleOnce(ctx, "evt-bad-miner", "run", "fail-camp", "not-an-address", "")
	if err == nil || !newly {
		t.Fatalf("want pay error with newly reservation attempt, newly=%v err=%v", newly, err)
	}
	ok, err := svc.HasFuzzSettleApplied(ctx, "evt-bad-miner")
	if err != nil || ok {
		t.Fatalf("failed pay must not leave applied mark: ok=%v err=%v", ok, err)
	}
	miner := "HMC-2222222222222222"
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}
	row, newly, err := svc.ApplyFuzzSettleOnce(ctx, "evt-bad-miner", "run", "fail-camp", miner, "")
	if err != nil || !newly || row.RunsDone != 1 {
		t.Fatalf("retry after transient: newly=%v runs=%v err=%v", newly, row, err)
	}
}
