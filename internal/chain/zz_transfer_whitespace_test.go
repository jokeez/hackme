package chain

// Parity with HMS #28 whitespace guard: SUP + transfer_v1 must reject padded From/To
// (sign payload trims; settle credits raw strings → stranded funds).

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

func TestValidateSupTransferShapeRejectsWhitespace(t *testing.T) {
	base := SupTransferTx{
		TxType:      "transfer_sup_v1",
		From:        "HMC-aaaaaaaaaaaaaaaa",
		To:          "HMC-bbbbbbbbbbbbbbbb",
		AmountUnits: 1,
		FeeUnits:    DefaultSUPTransferMinFee,
	}
	if code, _ := ValidateSupTransferShape(base); code != "" {
		t.Fatalf("clean addresses must pass: %q", code)
	}
	for _, pad := range []string{" ", "\t", "\n", "\r", "\r\n"} {
		tx := base
		tx.To = "HMC-bbbbbbbbbbbbbbbb" + pad
		code, msg := ValidateSupTransferShape(tx)
		if code != "invalid_address" || !strings.Contains(msg, "surrounding whitespace") {
			t.Fatalf("padded To %q: code=%q msg=%q", pad, code, msg)
		}
		tx = base
		tx.From = "HMC-aaaaaaaaaaaaaaaa" + pad
		code, msg = ValidateSupTransferShape(tx)
		if code != "invalid_address" || !strings.Contains(msg, "surrounding whitespace") {
			t.Fatalf("padded From %q: code=%q msg=%q", pad, code, msg)
		}
	}
}

func TestValidateTransferShapeRejectsWhitespace(t *testing.T) {
	base := TransferTx{
		TxType:        "transfer_v1",
		From:          "HMC-aaaaaaaaaaaaaaaa",
		To:            "HMC-bbbbbbbbbbbbbbbb",
		AmountUnits:   1,
		FeeUnits:      DefaultTransferMinFee,
		TimestampUnix: time.Now().Unix(),
	}
	if code, _ := ValidateTransferShape(base); code != "" {
		t.Fatalf("clean addresses must pass: %q", code)
	}
	tx := base
	tx.To = "HMC-bbbbbbbbbbbbbbbb "
	code, msg := ValidateTransferShape(tx)
	if code != "invalid_address" || !strings.Contains(msg, "surrounding whitespace") {
		t.Fatalf("padded To: code=%q msg=%q", code, msg)
	}
	tx = base
	tx.From = " HMC-aaaaaaaaaaaaaaaa"
	code, msg = ValidateTransferShape(tx)
	if code != "invalid_address" || !strings.Contains(msg, "surrounding whitespace") {
		t.Fatalf("padded From: code=%q msg=%q", code, msg)
	}
}

func TestSupTransferRejectsPaddedAddressAtSubmit(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := supTestWallet(t)
	pubA := privA.Public().(ed25519.PublicKey)
	cases := []struct {
		name string
		from string
		to   string
	}{
		{"trailing space in to", addrA, addrB + " "},
		{"leading space in to", addrA, " " + addrB},
		{"trailing tab in to", addrA, addrB + "\t"},
		{"padded from", addrA + " ", addrB},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := SupTransferTx{
				TxType:        "transfer_sup_v1",
				From:          tc.from,
				To:            tc.to,
				AmountUnits:   SUPToUnits(1.0),
				FeeUnits:      DefaultSUPTransferMinFee,
				Nonce:         0,
				TimestampUnix: time.Now().Unix(),
				PubKeyEd25519: hex.EncodeToString(pubA),
			}
			tx = signSupTransfer(t, tx, privA)
			_, code, err := svc.SubmitSupTransferTx(ctx, tx)
			if code != "invalid_address" || err == nil {
				t.Fatalf("want invalid_address, got code=%q err=%v", code, err)
			}
			if !strings.Contains(err.Error(), "surrounding whitespace") {
				t.Fatalf("want whitespace message, got %v", err)
			}
			var cnt int
			if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sup_tx_pool`).Scan(&cnt); err != nil {
				t.Fatal(err)
			}
			if cnt != 0 {
				t.Fatalf("padded tx must not enter pool: rows=%d", cnt)
			}
		})
	}
}

func TestSupTransferPaddedRowRejectedAtApply(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := supTestWallet(t)
	pubA := privA.Public().(ed25519.PublicKey)
	tx := SupTransferTx{
		TxType:        "transfer_sup_v1",
		From:          addrA,
		To:            addrB + " ",
		AmountUnits:   SUPToUnits(1.0),
		FeeUnits:      DefaultSUPTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signSupTransfer(t, tx, privA)
	h, err := tx.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO sup_tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status, reject_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', '')`,
		h, string(raw), tx.From, tx.To, tx.Nonce, tx.FeeUnits, tx.AmountUnits, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, e := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, "HMC-node", n, e, 0, m, ""); err != nil {
		t.Fatal(err)
	}
	var status, code string
	if err := svc.db.QueryRowContext(ctx, `SELECT status, reject_code FROM sup_tx_history WHERE tx_hash=?`, h).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != "rejected" || code != "invalid_address" {
		t.Fatalf("padded row must be rejected at apply: status=%q code=%q", status, code)
	}
	stA, err := svc.SupAddressState(ctx, addrA)
	if err != nil {
		t.Fatal(err)
	}
	if stA.BalanceSUPUnits != SUPToUnits(5.0) || stA.SUPNextNonce != 0 {
		t.Fatalf("sender untouched on reject: bal=%d nonce=%d", stA.BalanceSUPUnits, stA.SUPNextNonce)
	}
	var credited int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE address=?`, addrB+" ").Scan(&credited); err != nil {
		t.Fatal(err)
	}
	if credited != 0 {
		t.Fatalf("must not credit padded address: rows=%d", credited)
	}
}

func hmcWhitespaceWallet(t *testing.T) (*Service, string, ed25519.PrivateKey, string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "hmc-ws.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-node"); err != nil {
		t.Fatal(err)
	}
	pubA, privA, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubB, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	addrA, err := addressFromPubKeyHex(hex.EncodeToString(pubA))
	if err != nil {
		t.Fatal(err)
	}
	addrB, err := addressFromPubKeyHex(hex.EncodeToString(pubB))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))`, addrA, uint64(2_000_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaTotalMintedUnits, "2000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaTotalMintedHMC, "0.02"); err != nil {
		t.Fatal(err)
	}
	return svc, addrA, privA, addrB
}

func TestHMCTransferRejectsPaddedAddressAtSubmit(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := hmcWhitespaceWallet(t)
	pubA := privA.Public().(ed25519.PublicKey)
	tx := TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB + " ",
		AmountUnits:   500_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signTransfer(t, tx, privA)
	_, code, err := svc.SubmitTransferTx(ctx, tx)
	if code != "invalid_address" || err == nil {
		t.Fatalf("want invalid_address, got code=%q err=%v", code, err)
	}
	if !strings.Contains(err.Error(), "surrounding whitespace") {
		t.Fatalf("want whitespace message, got %v", err)
	}
	var cnt int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tx_pool`).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("padded tx must not enter pool: rows=%d", cnt)
	}
}

func TestHMCTransferPaddedRowRejectedAtApply(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := hmcWhitespaceWallet(t)
	pubA := privA.Public().(ed25519.PublicKey)
	tx := TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB + " ",
		AmountUnits:   500_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signTransfer(t, tx, privA)
	h, err := tx.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(ctx,
		`INSERT INTO tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status, reject_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', '')`,
		h, string(raw), tx.From, tx.To, tx.Nonce, tx.FeeUnits, tx.AmountUnits, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, e := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, "HMC-node", n, e, 0, m, ""); err != nil {
		t.Fatal(err)
	}
	var status, code string
	if err := svc.db.QueryRowContext(ctx, `SELECT status, reject_code FROM tx_history WHERE tx_hash=?`, h).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != "rejected" || code != "invalid_address" {
		t.Fatalf("padded row must be rejected at apply: status=%q code=%q", status, code)
	}
	a, err := svc.TransferAddressState(ctx, addrA)
	if err != nil {
		t.Fatal(err)
	}
	if a.BalanceUnits != 2_000_000 || a.NextNonce != 0 {
		t.Fatalf("sender untouched on reject: bal=%d nonce=%d", a.BalanceUnits, a.NextNonce)
	}
	var credited int
	if err := svc.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE address=?`, addrB+" ").Scan(&credited); err != nil {
		t.Fatal(err)
	}
	if credited != 0 {
		t.Fatalf("must not credit padded address: rows=%d", credited)
	}
}
