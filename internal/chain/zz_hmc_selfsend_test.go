package chain

// HMC self-transfer must not mint. validateTransferTx already rejects From==To
// (Low today). The applier still used a pre-debit absolute UPSERT, which mints
// the amount and drops the fee if that row is applied. SUP/HMS credit with SQL
// arithmetic and skip the recipient write when From==To; this file pins the
// same behavior on the HMC lane.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func TestHMCSelfTransferRejectedAtSubmit(t *testing.T) {
	ctx := context.Background()
	svc, addr, priv := hmcSelfWallet(t)
	tx := hmcSelfTx(t, addr, addr, 400_000, 0, priv)
	_, code, err := svc.SubmitTransferTx(ctx, tx)
	if err == nil || code != "invalid_address" {
		t.Fatalf("self-send must be rejected: code=%q err=%v", code, err)
	}
	st, err := svc.TransferAddressState(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if st.BalanceUnits != 2_000_000 || st.NextNonce != 0 {
		t.Fatalf("submit must not move funds: %+v", st)
	}
}

func TestHMCPooledSelfSendRejectedOnApply(t *testing.T) {
	ctx := context.Background()
	svc, addr, priv := hmcSelfWallet(t)
	tx := hmcSelfTx(t, addr, addr, 400_000, 0, priv)
	insertHMCPoolTx(t, svc, tx)
	before, err := svc.TransferAddressState(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	hmcSelfAppendBlock(t, svc)
	after, err := svc.TransferAddressState(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if after.BalanceUnits != before.BalanceUnits || after.NextNonce != before.NextNonce {
		t.Fatalf("rejected self-send moved funds: before=%+v after=%+v", before, after)
	}
	h, err := tx.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	var st string
	if err := svc.db.QueryRowContext(ctx, `SELECT status FROM tx_history WHERE tx_hash=?`, h).Scan(&st); err != nil || st != "rejected" {
		t.Fatalf("pooled self-send status=%q err=%v", st, err)
	}
}

func TestHMCApplierSelfSendDoesNotMint(t *testing.T) {
	ctx := context.Background()
	svc, addr, priv := hmcSelfWallet(t)
	const amount = uint64(400_000)
	tx := hmcSelfTx(t, addr, addr, amount, 0, priv)
	insertHMCPoolTx(t, svc, tx)
	before, err := svc.TransferAddressState(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	hmcApplyBypassValidate = true
	t.Cleanup(func() { hmcApplyBypassValidate = false })
	hmcSelfAppendBlock(t, svc)
	after, err := svc.TransferAddressState(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	total := amount + DefaultTransferMinFee
	if after.BalanceUnits > before.BalanceUnits {
		t.Fatalf("applier minted on self-send: before=%d after=%d", before.BalanceUnits, after.BalanceUnits)
	}
	if after.BalanceUnits != before.BalanceUnits-total {
		t.Fatalf("self-send debit: before=%d after=%d want %d", before.BalanceUnits, after.BalanceUnits, before.BalanceUnits-total)
	}
	if after.NextNonce != before.NextNonce+1 {
		t.Fatalf("nonce: got %d want %d", after.NextNonce, before.NextNonce+1)
	}
}

func hmcSelfWallet(t *testing.T) (*Service, string, ed25519.PrivateKey) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "hmc-self.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-node"); err != nil {
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
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))`, addr, uint64(2_000_000)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaTotalMintedUnits, "2000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaTotalMintedHMC, "0.02"); err != nil {
		t.Fatal(err)
	}
	return svc, addr, priv
}

func hmcSelfTx(t *testing.T, from, to string, amount, nonce uint64, priv ed25519.PrivateKey) TransferTx {
	t.Helper()
	tx := TransferTx{
		TxType:        "transfer_v1",
		From:          from,
		To:            to,
		AmountUnits:   amount,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         nonce,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(priv.Public().(ed25519.PublicKey)),
	}
	return signTransfer(t, tx, priv)
}

func insertHMCPoolTx(t *testing.T, svc *Service, tx TransferTx) {
	t.Helper()
	h, err := tx.HashHex()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(tx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.db.ExecContext(context.Background(),
		`INSERT INTO tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status, reject_code)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'pending', '')`,
		h, string(raw), tx.From, tx.To, tx.Nonce, tx.FeeUnits, tx.AmountUnits, time.Now().Unix()); err != nil {
		t.Fatal(err)
	}
}

func hmcSelfAppendBlock(t *testing.T, svc *Service) {
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
