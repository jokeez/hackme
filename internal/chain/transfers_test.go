package chain

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"hackme/internal/store"
)

func signTransfer(t *testing.T, tx TransferTx, priv ed25519.PrivateKey) TransferTx {
	t.Helper()
	b, err := tx.canonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	tx.SigEd25519 = hex.EncodeToString(ed25519.Sign(priv, b))
	return tx
}

func TestTransferSubmitAndApplyOnBlock(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tx.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
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
	// Keep economics meta consistent for transfer-fee burn invariants in tests.
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaTotalMintedUnits, "2000000"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, metaTotalMintedHMC, "0.02"); err != nil {
		t.Fatal(err)
	}
	tx := TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   500_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signTransfer(t, tx, privA)
	txHash, st, err := svc.SubmitTransferTx(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if st != "pending" || txHash == "" {
		t.Fatalf("submit status/hash: %s %q", st, txHash)
	}
	m, err := svc.PoHTargetMod(ctx)
	if err != nil {
		t.Fatal(err)
	}
	n, e := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, "HMC-node", n, e, 0, m, ""); err != nil {
		t.Fatal(err)
	}
	a, err := svc.TransferAddressState(ctx, addrA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.TransferAddressState(ctx, addrB)
	if err != nil {
		t.Fatal(err)
	}
	if a.NextNonce != 1 {
		t.Fatalf("sender nonce=%d", a.NextNonce)
	}
	if b.BalanceUnits != 500_000 {
		t.Fatalf("receiver balance=%d", b.BalanceUnits)
	}
	row, ok, err := svc.TransferTxByHash(ctx, txHash)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || row.Status != "included" {
		t.Fatalf("tx status: %+v ok=%v", row, ok)
	}
}

func TestTransferRejectBadNonce(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tx_bad_nonce.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	addrA, _ := addressFromPubKeyHex(hex.EncodeToString(pubA))
	addrB, _ := addressFromPubKeyHex(hex.EncodeToString(pubB))
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 3, strftime('%s','now'))`, addrA, uint64(2_000_000)); err != nil {
		t.Fatal(err)
	}
	tx := TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   10_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         2,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signTransfer(t, tx, privA)
	_, code, err := svc.SubmitTransferTx(ctx, tx)
	if err == nil {
		t.Fatal("expected nonce error")
	}
	if code != "invalid_nonce" {
		t.Fatalf("code=%s err=%v", code, err)
	}
}

func TestTransferRejectUnsupportedSigAlg(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tx_bad_alg.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	addrA, _ := addressFromPubKeyHex(hex.EncodeToString(pubA))
	addrB, _ := addressFromPubKeyHex(hex.EncodeToString(pubB))
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))`, addrA, uint64(2_000_000)); err != nil {
		t.Fatal(err)
	}
	tx := TransferTx{
		TxType:        "transfer_v1",
		SigAlg:        "dilithium3",
		From:          addrA,
		To:            addrB,
		AmountUnits:   10_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signTransfer(t, tx, privA)
	_, code, err := svc.SubmitTransferTx(ctx, tx)
	if err == nil || code != "unsupported_sig_alg" {
		t.Fatalf("expected unsupported_sig_alg, got code=%q err=%v", code, err)
	}
}

func TestTransferRejectInvalidSignature(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tx_bad_sig.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	pubA, _, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	addrA, _ := addressFromPubKeyHex(hex.EncodeToString(pubA))
	addrB, _ := addressFromPubKeyHex(hex.EncodeToString(pubB))
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))`, addrA, uint64(500_000)); err != nil {
		t.Fatal(err)
	}
	tx := TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   10_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
		SigEd25519:    strings.Repeat("00", ed25519.SignatureSize),
	}
	_, code, err := svc.SubmitTransferTx(ctx, tx)
	if err == nil || code != "invalid_signature" {
		t.Fatalf("want invalid_signature, got code=%s err=%v", code, err)
	}
}

func TestTransferRejectReplayDuplicate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tx_replay.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	addrA, _ := addressFromPubKeyHex(hex.EncodeToString(pubA))
	addrB, _ := addressFromPubKeyHex(hex.EncodeToString(pubB))
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))`, addrA, uint64(500_000)); err != nil {
		t.Fatal(err)
	}
	tx := signTransfer(t, TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   10_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}, privA)
	if _, _, err := svc.SubmitTransferTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	if _, code, err := svc.SubmitTransferTx(ctx, tx); err == nil || code != "duplicate_or_replay" {
		t.Fatalf("want duplicate_or_replay, got code=%s err=%v", code, err)
	}
}

func TestRejectStaleLocalPending(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "stale_pending.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	addrA, _ := addressFromPubKeyHex(hex.EncodeToString(pubA))
	addrB, _ := addressFromPubKeyHex(hex.EncodeToString(pubB))
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))`, addrA, uint64(5_000_000)); err != nil {
		t.Fatal(err)
	}
	tx0 := signTransfer(t, TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   10_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}, privA)
	if _, _, err := svc.SubmitTransferTx(ctx, tx0); err != nil {
		t.Fatal(err)
	}
	n, err := svc.RejectStaleLocalPending(ctx, addrA, 8)
	if err != nil || n != 1 {
		t.Fatalf("reject stale: n=%d err=%v", n, err)
	}
	pool, err := svc.TransferPool(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pool) != 0 {
		t.Fatalf("expected empty pending pool, got %d", len(pool))
	}
}

func TestTransferRejectPendingNonceConflict(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tx_pending_conflict.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	addrA, _ := addressFromPubKeyHex(hex.EncodeToString(pubA))
	addrB, _ := addressFromPubKeyHex(hex.EncodeToString(pubB))
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))`, addrA, uint64(5_000_000)); err != nil {
		t.Fatal(err)
	}
	tx1 := signTransfer(t, TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   10_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}, privA)
	tx2 := signTransfer(t, TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   20_000,
		FeeUnits:      DefaultTransferMinFee + 10,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}, privA)
	if _, _, err := svc.SubmitTransferTx(ctx, tx1); err != nil {
		t.Fatal(err)
	}
	if _, code, err := svc.SubmitTransferTx(ctx, tx2); err == nil || code != "pending_nonce_conflict" {
		t.Fatalf("want pending_nonce_conflict, got code=%s err=%v", code, err)
	}
}

func TestTransferConcurrentSameNonceOnlyOneAccepted(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tx_race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	pubA, privA, _ := ed25519.GenerateKey(rand.Reader)
	pubB, _, _ := ed25519.GenerateKey(rand.Reader)
	addrA, _ := addressFromPubKeyHex(hex.EncodeToString(pubA))
	addrB, _ := addressFromPubKeyHex(hex.EncodeToString(pubB))
	if _, err := db.ExecContext(ctx, `INSERT INTO accounts (address, balance_units, next_nonce, updated_at) VALUES (?, ?, 0, strftime('%s','now'))`, addrA, uint64(2_000_000)); err != nil {
		t.Fatal(err)
	}
	tx := signTransfer(t, TransferTx{
		TxType:        "transfer_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   10_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}, privA)
	var wg sync.WaitGroup
	var okN int
	var mu sync.Mutex
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := svc.SubmitTransferTx(ctx, tx); err == nil {
				mu.Lock()
				okN++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if okN != 1 {
		t.Fatalf("accepted=%d, want 1", okN)
	}
}

func TestTransferRejectAddressPubkeyMismatchForWalletAddress(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "tx_wallet_mismatch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-node"); err != nil {
		t.Fatal(err)
	}
	walletAddr, _, err := svc.Wallet(ctx)
	if err != nil {
		t.Fatal(err)
	}
	attackerPub, attackerPriv, _ := ed25519.GenerateKey(rand.Reader)
	recvPub, _, _ := ed25519.GenerateKey(rand.Reader)
	recvAddr, _ := addressFromPubKeyHex(hex.EncodeToString(recvPub))
	tx := signTransfer(t, TransferTx{
		TxType:        "transfer_v1",
		From:          walletAddr,
		To:            recvAddr,
		AmountUnits:   10_000,
		FeeUnits:      DefaultTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(attackerPub),
	}, attackerPriv)
	_, code, err := svc.SubmitTransferTx(ctx, tx)
	if err == nil {
		t.Fatal("expected address/pubkey mismatch rejection")
	}
	if code != "address_pubkey_mismatch" {
		t.Fatalf("want address_pubkey_mismatch, got code=%s err=%v", code, err)
	}
}
