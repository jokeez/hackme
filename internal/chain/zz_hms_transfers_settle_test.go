package chain

// HMS transfer settlement regression tests.
//
// Before #26, applyPendingHmsTransfers had zero call sites, so accepted HMS
// transfers stayed pending forever (the endpoint returned a tx hash and the
// pool was never drained), and ValidateHmsTransferShape did not reject
// From == To. #26 wired the applier and the self-send rejection; this file
// pins the settled behavior: transfers settle on the next PoH block (same as
// the HMC and SUP lanes), self-sends are rejected at submit (parity with the
// HMC lane and the report #30 SUP fix), and addresses with surrounding
// whitespace are rejected before they can strand funds.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/store"
)

func hmsSettleWallet(t *testing.T) (*Service, string, ed25519.PrivateKey) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "hms-settle.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-node"); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitHMSGenesis(ctx, "HMC-aaaaaaaaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	addr, err := addressFromPubKeyHex(hex.EncodeToString(pub))
	if err != nil {
		t.Fatal(err)
	}
	if code, err := svc.MintHMS(ctx, addr, HMSToUnits(5.0), "fund"); err != nil || code != "" {
		t.Fatalf("MintHMS: code=%q err=%v", code, err)
	}
	return svc, addr, priv
}

func hmsSettleTx(t *testing.T, svc *Service, from, to string, amount, nonce uint64, priv ed25519.PrivateKey) HmsTransferTx {
	t.Helper()
	tx := HmsTransferTx{
		TxType:        "transfer_hms_v1",
		From:          from,
		To:            to,
		AmountUnits:   amount,
		FeeUnits:      DefaultHMSTransferMinFee,
		Nonce:         nonce,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
	}
	b, err := tx.canonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	tx.SigEd25519 = hex.EncodeToString(ed25519.Sign(priv, b))
	return tx
}

func hmsSettleAppendBlock(t *testing.T, svc *Service) {
	t.Helper()
	ctx := context.Background()
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, e := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, "HMC-node", n, e, 0, m, ""); err != nil {
		t.Fatal(err)
	}
}

func TestHMSTransferSettlesOnBlockAppend(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA := hmsSettleWallet(t)
	const toB = "HMC-cccccccccccccccc"
	tx := hmsSettleTx(t, svc, addrA, toB, HMSToUnits(1.0), 0, privA)
	txHash, st, err := svc.SubmitHmsTransferTx(ctx, tx)
	if err != nil || st != "pending" {
		t.Fatalf("submit: st=%q err=%v", st, err)
	}
	hmsSettleAppendBlock(t, svc)

	var pending int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM hms_tx_pool WHERE status='pending'`).Scan(&pending); err != nil {
		t.Fatal(err)
	}
	if pending != 0 {
		t.Fatalf("block applied but HMS pool not drained: pending=%d", pending)
	}
	stB, err := svc.HmsAddressState(ctx, toB)
	if err != nil || stB.BalanceHMSUnits != HMSToUnits(1.0) {
		t.Fatalf("recipient not credited: units=%d err=%v", stB.BalanceHMSUnits, err)
	}
	stA, err := svc.HmsAddressState(ctx, addrA)
	if err != nil {
		t.Fatal(err)
	}
	want := HMSToUnits(5.0) - HMSToUnits(1.0) - DefaultHMSTransferMinFee
	if stA.BalanceHMSUnits != want {
		t.Fatalf("sender balance: got %d want %d (amount+fee debited once)", stA.BalanceHMSUnits, want)
	}
	if stA.HMSNextNonce != 1 {
		t.Fatalf("sender nonce: got %d want 1", stA.HMSNextNonce)
	}
	var hist int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM hms_tx_history WHERE tx_hash=? AND status='included'`, txHash).Scan(&hist); err != nil {
		t.Fatal(err)
	}
	if hist != 1 {
		t.Fatalf("included history rows for the settled tx: got %d want 1", hist)
	}
}

func TestHMSSelfTransferRejectedAtSubmit(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA := hmsSettleWallet(t)
	tx := hmsSettleTx(t, svc, addrA, addrA, HMSToUnits(4.0), 0, privA)
	hash, st, err := svc.SubmitHmsTransferTx(ctx, tx)
	if st != "invalid_address" || err == nil {
		t.Fatalf("self-send must be rejected with invalid_address: st=%q hash=%q err=%v", st, hash, err)
	}
	var cnt int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM hms_tx_pool`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("self-send must not enter the pool: rows=%d", cnt)
	}
}

func TestHMSTransferRejectsPaddedAddress(t *testing.T) {
	cases := []struct {
		name string
		pad  string // "from" = append a space to From
		to   string
	}{
		{"trailing space in to", "", "HMC-cccccccccccccccc "},
		{"leading space in to", "", " HMC-cccccccccccccccc"},
		{"trailing tab in to", "", "HMC-cccccccccccccccc\t"},
		{"trailing newline in to", "", "HMC-cccccccccccccccc\n"},
		{"trailing crlf in to", "", "HMC-cccccccccccccccc\r\n"},
		{"trailing carriage return in to", "", "HMC-cccccccccccccccc\r"},
		{"padded from", "from", "HMC-cccccccccccccccc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			svc, addrA, privA := hmsSettleWallet(t)
			from := addrA
			if tc.pad == "from" {
				from = addrA + " "
			}
			// canonicalBytes trims From/To for signing, so the tx carries a valid
			// signature over the trimmed form while the raw payload is padded.
			tx := hmsSettleTx(t, svc, from, tc.to, HMSToUnits(1.0), 0, privA)
			_, st, err := svc.SubmitHmsTransferTx(ctx, tx)
			if st != "invalid_address" || err == nil {
				t.Fatalf("padded address must be rejected with invalid_address: st=%q err=%v", st, err)
			}
			if !strings.Contains(err.Error(), "surrounding whitespace") {
				t.Fatalf("want the whitespace guard to fire, got err=%v", err)
			}
			var cnt int
			if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM hms_tx_pool`).Scan(&cnt); err != nil {
				t.Fatal(err)
			}
			if cnt != 0 {
				t.Fatalf("padded-address tx must not enter the pool: rows=%d", cnt)
			}
		})
	}
}

func TestHMSTransferPaddedRowRejectedAtApply(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA := hmsSettleWallet(t)
	// Emulate a padded-transfer row that entered hms_tx_pool before this guard
	// shipped: sign it over the trimmed form exactly like a wallet that
	// accepted it pre-guard, then insert it directly, bypassing submit-time
	// validation. With the guard, shape validation runs before the signature
	// check at apply time, so the padded To is rejected; without it, the row
	// settles and credits the raw padded address.
	tx := hmsSettleTx(t, svc, addrA, "HMC-cccccccccccccccc ", HMSToUnits(1.0), 0, privA)
	h, err := tx.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO hms_tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status, reject_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', '')`,
		h, string(raw), tx.From, tx.To, tx.Nonce, tx.FeeUnits, tx.AmountUnits, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	hmsSettleAppendBlock(t, svc)

	var status, code string
	if err := svc.db.QueryRowContext(ctx, `SELECT status, reject_code FROM hms_tx_history WHERE tx_hash=?`, h).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != "rejected" || code != "invalid_address" {
		t.Fatalf("padded row must be rejected at apply: status=%q code=%q", status, code)
	}
	var rows int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM hms_tx_pool`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("padded row must be deleted from the pool: rows=%d", rows)
	}
	var credited int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE address=?`, "HMC-cccccccccccccccc ").Scan(&credited); err != nil {
		t.Fatal(err)
	}
	if credited != 0 {
		t.Fatalf("no balance must be credited to the raw padded address: rows=%d", credited)
	}
	stA, err := svc.HmsAddressState(ctx, addrA)
	if err != nil {
		t.Fatal(err)
	}
	if stA.BalanceHMSUnits != HMSToUnits(5.0) {
		t.Fatalf("sender must not be debited when the row is rejected: units=%d", stA.BalanceHMSUnits)
	}
	if stA.HMSNextNonce != 0 {
		t.Fatalf("sender nonce must be untouched when the row is rejected: nonce=%d", stA.HMSNextNonce)
	}
}
