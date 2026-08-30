package chain

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"testing"
	"time"

	"hackme/internal/store"
)

func signSupTransfer(t *testing.T, tx SupTransferTx, priv ed25519.PrivateKey) SupTransferTx {
	t.Helper()
	b, err := tx.canonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	tx.SigEd25519 = hex.EncodeToString(ed25519.Sign(priv, b))
	return tx
}

func supTestWallet(t *testing.T) (*Service, string, ed25519.PrivateKey, string) {
	t.Helper()
	ctx := context.Background()
	db, err := store.Open(filepath.Join(t.TempDir(), "sup-transfer.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	svc := New(db)
	if _, _, err := svc.InitGenesis(ctx, "HMC-node"); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitSUPGenesis(ctx); err != nil {
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
	if code, err := svc.MintSUP(ctx, addrA, SUPToUnits(5.0), "fund"); err != nil || code != "" {
		t.Fatalf("MintSUP: code=%q err=%v", code, err)
	}
	return svc, addrA, privA, addrB
}

func TestSupTransferSubmitAndApplyOnBlock(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := supTestWallet(t)
	pubA := privA.Public().(ed25519.PublicKey)
	tx := SupTransferTx{
		TxType:        "transfer_sup_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   SUPToUnits(1.0),
		FeeUnits:      DefaultSUPTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signSupTransfer(t, tx, privA)
	txHash, st, err := svc.SubmitSupTransferTx(ctx, tx)
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
	a, err := svc.SupAddressState(ctx, addrA)
	if err != nil {
		t.Fatal(err)
	}
	b, err := svc.SupAddressState(ctx, addrB)
	if err != nil {
		t.Fatal(err)
	}
	if a.SUPNextNonce != 1 {
		t.Fatalf("sender sup nonce=%d", a.SUPNextNonce)
	}
	if b.BalanceSUPUnits != SUPToUnits(1.0) {
		t.Fatalf("recipient balance=%d", b.BalanceSUPUnits)
	}
}

func TestSupTransferRejectUnsigned(t *testing.T) {
	ctx := context.Background()
	svc, addrA, _, addrB := supTestWallet(t)
	tx := SupTransferTx{
		TxType:        "transfer_sup_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   SUPToUnits(0.5),
		FeeUnits:      DefaultSUPTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
	}
	_, code, err := svc.SubmitSupTransferTx(ctx, tx)
	if err == nil || code != "invalid_signature" {
		t.Fatalf("want invalid_signature got code=%q err=%v", code, err)
	}
}

func TestSupTransferRejectFeeTooLow(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := supTestWallet(t)
	pubA := privA.Public().(ed25519.PublicKey)
	tx := SupTransferTx{
		TxType:        "transfer_sup_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   SUPToUnits(0.5),
		FeeUnits:      DefaultSUPTransferMinFee - 1,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signSupTransfer(t, tx, privA)
	_, code, err := svc.SubmitSupTransferTx(ctx, tx)
	if err == nil || code != "fee_too_low" {
		t.Fatalf("want fee_too_low got code=%q err=%v", code, err)
	}
}

func TestSupTransferRejectInsufficientBalance(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := supTestWallet(t)
	pubA := privA.Public().(ed25519.PublicKey)
	tx := SupTransferTx{
		TxType:        "transfer_sup_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   SUPToUnits(100.0),
		FeeUnits:      DefaultSUPTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signSupTransfer(t, tx, privA)
	_, code, err := svc.SubmitSupTransferTx(ctx, tx)
	if err == nil || code != "insufficient_sup_balance" {
		t.Fatalf("want insufficient_sup_balance got code=%q err=%v", code, err)
	}
}

func TestSupTransferRejectDuplicateNonce(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := supTestWallet(t)
	pubA := privA.Public().(ed25519.PublicKey)
	tx0 := SupTransferTx{
		TxType:        "transfer_sup_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   SUPToUnits(0.1),
		FeeUnits:      DefaultSUPTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx0 = signSupTransfer(t, tx0, privA)
	if _, _, err := svc.SubmitSupTransferTx(ctx, tx0); err != nil {
		t.Fatal(err)
	}
	tx1 := tx0
	tx1.To = addrB
	tx1.AmountUnits = SUPToUnits(0.2)
	tx1 = signSupTransfer(t, tx1, privA)
	_, code, err := svc.SubmitSupTransferTx(ctx, tx1)
	if err == nil || code != "pending_nonce_conflict" {
		t.Fatalf("want pending_nonce_conflict got code=%q err=%v", code, err)
	}
}

func TestSupWalletEarningsMintAndTransfer(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := supTestWallet(t)
	if code, err := svc.MintSUP(ctx, addrA, SUPToUnits(2.0), "worker_sup_settlement:test:delta=2"); err != nil || code != "" {
		t.Fatalf("settlement mint: %v %s", err, code)
	}
	pubA := privA.Public().(ed25519.PublicKey)
	tx := SupTransferTx{
		TxType:        "transfer_sup_v1",
		From:          addrA,
		To:            addrB,
		AmountUnits:   SUPToUnits(0.5),
		FeeUnits:      DefaultSUPTransferMinFee,
		Nonce:         0,
		TimestampUnix: time.Now().Unix(),
		PubKeyEd25519: hex.EncodeToString(pubA),
	}
	tx = signSupTransfer(t, tx, privA)
	if _, _, err := svc.SubmitSupTransferTx(ctx, tx); err != nil {
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
	ec, err := svc.SupWalletEarningsSummary(ctx, addrA, 24, 3600)
	if err != nil {
		t.Fatal(err)
	}
	if ec.TotalReceivedHMC < 6.99 {
		t.Fatalf("received want ~7 got %v", ec.TotalReceivedHMC)
	}
	if ec.SettledOutWindowHMC < 1.99 {
		t.Fatalf("pool minted want ~2 got %v", ec.SettledOutWindowHMC)
	}
	if ec.TotalSentHMC < 0.004999 {
		t.Fatalf("sent want transfer+fee got %v", ec.TotalSentHMC)
	}
	if len(ec.Buckets) == 0 {
		t.Fatal("expected buckets")
	}
}
