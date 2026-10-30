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

func TestWalletInboundPageCursor(t *testing.T) {
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	ctx := context.Background()
	addr := "HMC-dddddddddddddddd"
	from := "HMC-eeeeeeeeeeeeeeee"
	now := time.Now().Unix()
	for _, a := range []string{addr, from} {
		if _, err := db.ExecContext(ctx,
			`INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, 0, 0, ?)`,
			a, now); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		hash := "hash-page-" + string(rune('a'+i))
		if _, err := db.ExecContext(ctx,
			`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, applied_at)
			 VALUES (?, '{}', ?, ?, 0, 1000, ?, 'included', ?)`,
			hash, from, addr, uint64(1_000_000*(i+1)), now-int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	// Outbound must not appear on the inbound page.
	if _, err := db.ExecContext(ctx,
		`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, applied_at)
		 VALUES ('hash-out', '{}', ?, ?, 0, 1000, 9, 'included', ?)`,
		addr, from, now); err != nil {
		t.Fatal(err)
	}
	page, err := svc.WalletInboundPage(ctx, addr, 24, 2, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Recent) != 2 {
		t.Fatalf("page1 %d", len(page.Recent))
	}
	if page.Recent[0].TxHash != "hash-page-a" || page.Recent[1].TxHash != "hash-page-b" {
		t.Fatalf("order %+v", page.Recent)
	}
	last := page.Recent[len(page.Recent)-1]
	page2, err := svc.WalletInboundPage(ctx, addr, 24, 2, last.AppliedUnix, last.TxHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Recent) != 2 || page2.Recent[0].TxHash != "hash-page-c" {
		t.Fatalf("page2 %+v", page2.Recent)
	}
	for _, ev := range append(page.Recent, page2.Recent...) {
		if ev.Direction != "in" || ev.AmountUnits == 0 {
			t.Fatalf("bad event %+v", ev)
		}
	}
}
