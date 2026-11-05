package chain

import (
	"context"
	"path/filepath"
	"testing"

	"hackme/internal/store"
)

func TestTransfersByFromNonce(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "chain.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	ctx := context.Background()
	from, to := "HMC-aaaaaaaaaaaaaaaa", "HMC-bbbbbbbbbbbbbbbb"
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, applied_at)
	      VALUES ('h7', '{"memo":"worker_settlement"}', ?, ?, 7, 1, 500, 'included', 1)`, from, to)
	exec(`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, applied_at)
	      VALUES ('h8', '{}', ?, ?, 8, 1, 600, 'included', 2)`, from, to)
	exec(`INSERT INTO tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status)
	      VALUES ('p9', '{}', ?, ?, 9, 1, 700, 3, 'pending')`, from, to)

	got, err := svc.TransfersByFromNonce(ctx, from, 7)
	if err != nil || len(got) != 1 || got[0].TxHash != "h7" || got[0].Status != "included" || got[0].AmountUnits != 500 || got[0].To != to {
		t.Fatalf("nonce 7: %+v err=%v", got, err)
	}
	if got, _ := svc.TransfersByFromNonce(ctx, from, 9); len(got) != 1 || got[0].TxHash != "p9" || got[0].Status != "pending" {
		t.Fatalf("nonce 9 (pool): %+v", got)
	}
	if got, _ := svc.TransfersByFromNonce(ctx, from, 10); len(got) != 0 {
		t.Fatalf("nonce 10: %+v", got)
	}
	if got, _ := svc.TransfersByFromNonce(ctx, to, 7); len(got) != 0 {
		t.Fatalf("other sender: %+v", got)
	}
}
