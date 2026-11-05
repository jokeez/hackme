package chain

// Audit 9 (money logic: HMS/SUP) — failing tests that pin open findings.

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/hms"
	"hackme/internal/store"
)

// A1: a transfer rejected at apply is moved to hms_tx_history (status=rejected). Submit only
// checks the pool, so the identical signed tx (same hash: the hash excludes the signature and
// the nonce did not advance) is accepted again. When it then applies, the 'included' history
// INSERT hits UNIQUE(tx_hash), applyPendingHmsTransfers returns an error and AppendPoHBlock
// fails — on every retry, because the pool row stays pending: the chain halts.
func TestAudit9_RejectedTxResubmitHaltsBlockProduction(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA := hmsSettleWallet(t) // 5 HMS
	const toB = "HMC-cccccccccccccccc"
	tx := hmsSettleTx(t, svc, addrA, toB, HMSToUnits(4.0), 0, privA)
	if _, st, err := svc.SubmitHmsTransferTx(ctx, tx); err != nil || st != "pending" {
		t.Fatalf("submit: %q %v", st, err)
	}
	// Any debit between submit and apply (today: an internal burn; tomorrow: a public SUP/HMS
	// fee product) makes the pending tx fail validation at apply.
	if code, err := svc.BurnHMSForService(ctx, addrA, HMSToUnits(2.0), "drain"); err != nil || code != "" {
		t.Fatalf("burn: %q %v", code, err)
	}
	hmsSettleAppendBlock(t, svc) // tx rejected (insufficient balance)
	h, _ := tx.HashHex()
	var st string
	_ = svc.db.QueryRowContext(ctx, `SELECT status FROM hms_tx_history WHERE tx_hash=?`, h).Scan(&st)
	if st != "rejected" {
		t.Fatalf("expected rejected history row, got %q", st)
	}
	// Refill and resubmit the very same signed bytes.
	if code, err := svc.MintHMS(ctx, addrA, HMSToUnits(5.0), "refill"); err != nil || code != "" {
		t.Fatalf("refill: %q %v", code, err)
	}
	if _, st, err := svc.SubmitHmsTransferTx(ctx, tx); err != nil {
		t.Logf("resubmit refused (fixed): %q %v", st, err)
		return
	}
	m, _ := svc.PoHTargetMod(ctx)
	n, e := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, "HMC-node", n, e, 0, m, ""); err != nil {
		t.Fatalf("FINDING: resubmitted rejected tx halts block production: %v", err)
	}
}

// A2: PayHMSStorageMarket (still) books a counter-only HMC burn after Economics v2: burn_hmc is
// added to total_burned_units, but no units leave any balance for it (the debit is
// storage+fee; storage goes to the never-released hms_market_escrow_units counter, fee to the
// dev address). total_burned overstates real destruction by 10% of every storage order.
func TestAudit9_HMSMarketPhantomBurn(t *testing.T) {
	t.Setenv("HMS_MARKET_PAYMENT_HMAC_SECRET", "s")
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, DevFeeAddress); err != nil {
		t.Fatal(err)
	}
	var w0, d0 uint64
	_ = db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&w0)
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(balance_units,0) FROM accounts WHERE address=?`, DevFeeAddress).Scan(&d0)
	_, b0, _ := svc.econTotalsUnits(ctx, db)
	q, _ := hms.QuoteStorageOrder(1<<30, 30)
	if _, err := svc.PayHMSStorageMarket(ctx, "x", 1<<30, 30, q.QuoteHash, "k"); err != nil {
		t.Fatal(err)
	}
	var w1, d1 uint64
	_ = db.QueryRowContext(ctx, `SELECT balance_units FROM wallet WHERE id=1`).Scan(&w1)
	_ = db.QueryRowContext(ctx, `SELECT COALESCE(balance_units,0) FROM accounts WHERE address=?`, DevFeeAddress).Scan(&d1)
	_, b1, _ := svc.econTotalsUnits(ctx, db)
	esc, _ := svc.metaUint(ctx, db, metaHMSMarketEscrowUnits, 0)
	// Genesis wallet == DevFeeAddress here, so the fee comes straight back to the wallet row.
	_ = d0
	_ = d1
	left := w0 - w1 // units that left circulating balances
	if left != esc+(b1-b0) {
		t.Fatalf("FINDING: debit %d = escrow %d + burned %d does not hold (phantom burn %d units)",
			left, esc, b1-b0, esc+(b1-b0)-left)
	}
}
