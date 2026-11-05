package chain

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func audit7Bal(t *testing.T, ctx context.Context, svc *Service, addr string) uint64 {
	var u uint64
	_ = svc.db.QueryRowContext(ctx, `SELECT COALESCE(balance_units,0) FROM accounts WHERE address=?`, addr).Scan(&u)
	return u
}

// Reopen re-locks only the run pool, but the escrow row still carries the full bounty pool,
// so a second finalize refunds the bounty pool a second time (mint), and a finding after
// reopen pays the already-refunded bounty pool.
func TestAudit7ReopenDoubleRefundsBounty(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "a7.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	payer := "HMC-1111111111111111"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 50)
	const camp = "a7-camp"
	start := audit7Bal(t, ctx, svc, payer)
	if _, err := svc.OpenFuzzEscrow(ctx, camp, 10.0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FinalizeFuzzEscrow(ctx, camp); err != nil {
		t.Fatal(err)
	}
	if b := audit7Bal(t, ctx, svc, payer); b != start {
		t.Fatalf("after first finalize payer=%d want %d", b, start)
	}
	if _, err := svc.ReopenFuzzEscrowForSettleDrain(ctx, camp); err != nil {
		t.Fatal(err)
	}
	// Variant A: second finalize (cleanup-stale / campaign completed path)
	if _, err := svc.FinalizeFuzzEscrow(ctx, camp); err != nil {
		t.Fatal(err)
	}
	end := audit7Bal(t, ctx, svc, payer)
	t.Logf("payer start=%d end=%d delta=%+d units", start, end, int64(end)-int64(start))
	if end != start {
		t.Fatalf("payer gained %d units from reopen+finalize (bounty refunded twice)", int64(end)-int64(start))
	}
}

func TestAudit7ReopenThenFindingPaysRefundedBounty(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "a7b.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	payer := "HMC-1111111111111111"
	miner := "HMC-2222222222222222"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 50)
	const camp = "a7-camp2"
	start := audit7Bal(t, ctx, svc, payer)
	dev0 := audit7Bal(t, ctx, svc, DevFeeAddress)
	if _, err := svc.OpenFuzzEscrow(ctx, camp, 10.0, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.FinalizeFuzzEscrow(ctx, camp); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ReopenFuzzEscrowForSettleDrain(ctx, camp); err != nil {
		t.Fatal(err)
	}
	// The bounty pool already went back to the payer: the finding must be a terminal no-op
	// (Depleted → ACKed by the pull drain), never a second payout of refunded units.
	if _, _, err := svc.ApplyFuzzSettleOnce(ctx, "outbox:a7-camp2:9", "finding", camp, miner, "critical"); !errors.Is(err, ErrFuzzEscrowDepleted) {
		t.Fatalf("finding after reopen must be depleted, got %v", err)
	}
	if _, err := svc.FinalizeFuzzEscrow(ctx, camp); err != nil {
		t.Fatal(err)
	}
	end := audit7Bal(t, ctx, svc, payer)
	m := audit7Bal(t, ctx, svc, miner)
	dev := audit7Bal(t, ctx, svc, DevFeeAddress) - dev0
	t.Logf("payer delta=%+d miner=%d dev=%d", int64(end)-int64(start), m, dev)
	if int64(end)-int64(start)+int64(m)+int64(dev) != 0 {
		t.Fatalf("value created: payer delta %+d + miner %d + dev %d != 0", int64(end)-int64(start), m, dev)
	}
}

// Second finding after the bounty is paid returns ErrFuzzEscrowClosed (not AlreadyPaid):
// pull mode pre-filters by status, but the HTTP relay leaves the row pending forever on 409.
func TestAudit7SecondFindingErrClass(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "a7c.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	payer := "HMC-1111111111111111"
	if _, _, err := svc.InitGenesis(ctx, payer); err != nil {
		t.Fatal(err)
	}
	preFundEscrow(t, ctx, db, payer, 50)
	if _, err := svc.OpenFuzzEscrow(ctx, "c", 10.0, 100); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.ApplyFuzzSettleOnce(ctx, "outbox:c:1", "finding", "c", "HMC-2222222222222222", "high"); err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.ApplyFuzzSettleOnce(ctx, "outbox:c:2", "finding", "c", "HMC-3333333333333333", "high")
	t.Logf("second finding err=%v closed=%v alreadyPaid=%v drainable=%v", err, errors.Is(err, ErrFuzzEscrowClosed), errors.Is(err, ErrFuzzEscrowAlreadyPaid), isFuzzSettleDrainErr(err))
}
