package chain

import (
	"context"
	"fmt"
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

	eventID := FuzzSettleEventID("once-camp", 99)
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

func TestApplyFuzzSettleOnceCrashBonusNoDoublePay(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "once-crash.db"))
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
	if _, err := svc.OpenFuzzEscrow(ctx, "once-crash", 10.0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}

	row, newly, err := svc.ApplyFuzzSettleOnce(ctx, "crash-evt-1", "crash_bonus", "once-crash", miner, "")
	if err != nil || !newly {
		t.Fatalf("first crash bonus: newly=%v err=%v", newly, err)
	}
	if row == nil || row.CrashBonusPaidHMC <= 0 || row.CrashBonusPaidHMC > 0.0100001 {
		t.Fatalf("crash bonus out of cap: %+v", row)
	}
	paid := row.CrashBonusPaidHMC

	row, newly, err = svc.ApplyFuzzSettleOnce(ctx, "crash-evt-1", "crash_bonus", "once-crash", miner, "")
	if err != nil || newly {
		t.Fatalf("replay same event: newly=%v err=%v", newly, err)
	}
	if row.CrashBonusPaidHMC != paid {
		t.Fatalf("double-pay via replay: %v -> %v", paid, row.CrashBonusPaidHMC)
	}

	_, newly, err = svc.ApplyFuzzSettleOnce(ctx, "crash-evt-2", "crash_bonus", "once-crash", miner, "")
	if newly && err == nil {
		t.Fatal("second crash_bonus event must not pay again")
	}
}

// TestApplyFuzzSettleOnceCrossCampaignOutboxIDNoCollision reproduces the midnight-critical bug:
// bootstrap already applied legacy outbox:<id> (and/or another campaign reused the same
// numeric outbox id). New campaigns must still credit when event_id includes campaign_id.
func TestApplyFuzzSettleOnceCrossCampaignOutboxIDNoCollision(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "collision.db"))
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
	if _, err := svc.OpenFuzzEscrow(ctx, "camp-old", 10.0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.OpenFuzzEscrow(ctx, "camp-new", 10.0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO accounts(address, balance_units, next_nonce, updated_at) VALUES(?,0,0,0)`, miner); err != nil {
		t.Fatal(err)
	}

	const outboxID int64 = 1
	// Legacy bootstrap mark (pre-fix format) already present on customer node.
	legacyID := fmt.Sprintf("outbox:%d", outboxID)
	if newly, err := svc.MarkFuzzSettleApplied(ctx, legacyID, "camp-old", "run"); err != nil || !newly {
		t.Fatalf("seed legacy applied: newly=%v err=%v", newly, err)
	}
	// Same numeric outbox id already applied for another campaign under the new format.
	oldCampID := FuzzSettleEventID("camp-old", outboxID)
	row, newly, err := svc.ApplyFuzzSettleOnce(ctx, oldCampID, "run", "camp-old", miner, "")
	if err != nil || !newly || row == nil || row.RunsDone != 1 {
		t.Fatalf("camp-old apply: newly=%v runs=%v err=%v", newly, row, err)
	}

	// New campaign reuses low outbox id on a fresh coordinator DB — must still pay.
	newCampID := FuzzSettleEventID("camp-new", outboxID)
	if newCampID == legacyID || newCampID == oldCampID {
		t.Fatalf("event ids must differ: legacy=%q old=%q new=%q", legacyID, oldCampID, newCampID)
	}
	row, newly, err = svc.ApplyFuzzSettleOnce(ctx, newCampID, "run", "camp-new", miner, "")
	if err != nil || !newly {
		t.Fatalf("camp-new must credit despite reused outbox id: newly=%v err=%v", newly, err)
	}
	if row == nil || row.RunsDone != 1 {
		t.Fatalf("camp-new runs_done=%v want 1 (collision would leave 0)", row)
	}
	// Idempotent on redelivery of the same new-format event.
	row, newly, err = svc.ApplyFuzzSettleOnce(ctx, newCampID, "run", "camp-new", miner, "")
	if err != nil || newly || row.RunsDone != 1 {
		t.Fatalf("camp-new replay: newly=%v runs=%v err=%v", newly, row, err)
	}
}
