package chain

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestWalletActivitySummaryCounterparties(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	ctx := context.Background()

	addrSelf := "HMC-aaaaaaaaaaaaaaaa"
	addrIn := "HMC-bbbbbbbbbbbbbbbb"
	addrOut := "HMC-cccccccccccccccc"
	now := time.Now().Unix()

	for _, q := range []string{
		`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, 0, 0, ?)`,
	} {
		for _, a := range []string{addrSelf, addrIn, addrOut} {
			if _, err := db.ExecContext(ctx, q, a, now); err != nil {
				t.Fatal(err)
			}
		}
	}

	insertTx := func(hash, from, to string, amount, fee uint64, applied int64) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, applied_at)
			 VALUES (?, '{}', ?, ?, 0, ?, ?, 'included', ?)`,
			hash, from, to, fee, amount, applied,
		)
		if err != nil {
			t.Fatal(err)
		}
	}
	insertTx("hash-in-1", addrIn, addrSelf, 100_000_000, 1000, now-100)
	insertTx("hash-in-2", addrIn, addrSelf, 50_000_000, 1000, now-50)
	insertTx("hash-out-1", addrSelf, addrOut, 10_000_000, 1000, now-10)

	sum, err := svc.WalletActivitySummary(ctx, addrSelf, 24, 10)
	if err != nil {
		t.Fatal(err)
	}
	if sum.TxCountWindow != 3 {
		t.Fatalf("tx count: %d", sum.TxCountWindow)
	}
	if len(sum.Counterparties) != 2 {
		t.Fatalf("counterparties: %d", len(sum.Counterparties))
	}
	if sum.TotalReceivedHMC < 1.49 || sum.TotalReceivedHMC > 1.51 {
		t.Fatalf("recv: %v", sum.TotalReceivedHMC)
	}
	if len(sum.Recent) != 3 {
		t.Fatalf("recent: %d", len(sum.Recent))
	}
	if sum.Recent[0].AmountUnits == 0 {
		t.Fatalf("missing amount_units on recent event: %+v", sum.Recent[0])
	}
	foundIn := false
	for _, cp := range sum.Counterparties {
		if cp.Peer == addrIn && cp.TxCount == 2 && cp.ReceivedHMC > 1.4 {
			foundIn = true
		}
	}
	if !foundIn {
		t.Fatalf("missing inbound peer: %#v", sum.Counterparties)
	}
}
