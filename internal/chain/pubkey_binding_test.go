package chain

import (
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/nodecrypto"
	"hackme/internal/store"
)

const otherPubKey = "1111111111111111111111111111111111111111111111111111111111111111"

func boundKey(t *testing.T, db *sql.DB, addr string) (pub, source string) {
	t.Helper()
	err := db.QueryRow(`SELECT pubkey_ed25519, source FROM address_pubkeys WHERE address=?`, addr).Scan(&pub, &source)
	if err != nil && err != sql.ErrNoRows {
		t.Fatal(err)
	}
	return
}

func preBind(t *testing.T, db *sql.DB, addr, pub string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO address_pubkeys (address, pubkey_ed25519, bound_at_index, source) VALUES (?, ?, 0, 'test')`, addr, pub); err != nil {
		t.Fatal(err)
	}
}

func queueTransfer(t *testing.T, db *sql.DB, tx TransferTx) string {
	t.Helper()
	raw, _ := json.Marshal(tx)
	h, _ := tx.HashHex()
	if _, err := db.Exec(`INSERT INTO tx_pool (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, received_at, status, reject_code)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, 'pending', '')`, h, string(raw), tx.From, tx.To, tx.Nonce, tx.FeeUnits, tx.AmountUnits); err != nil {
		t.Fatal(err)
	}
	return h
}

// The first included spend binds its key; the binding is what later spends are checked against.
func TestPubKeyBindingFirstSpendBinds(t *testing.T) {
	n := newV2Net(t, "1")
	fundAddr(t, n.hubDB, n.signer.Address(), HMCToUnits(1))
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, signedTransfer(t, n.signer, "HMC-ec00000000000010", 0, V2ChainID)); err != nil {
		t.Fatalf("submit: %s %v", code, err)
	}
	n.mine()
	pub, src := boundKey(t, n.hubDB, n.signer.Address())
	if pub != n.signer.PublicKeyHex() || src != PubKeyBindSourceHMC {
		t.Fatalf("binding %q/%q", pub, src)
	}
	// Same key keeps spending.
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, signedTransfer(t, n.signer, "HMC-ec00000000000010", 1, V2ChainID)); err != nil {
		t.Fatalf("second spend: %s %v", code, err)
	}
}

// A spend signed by a key other than the bound one is refused at submit and, if it is already
// queued, at block apply — the funds stay put.
func TestPubKeyBindingMismatchRejectedFromV2(t *testing.T) {
	n := newV2Net(t, "1")
	addr := n.signer.Address()
	fundAddr(t, n.hubDB, addr, HMCToUnits(1))
	preBind(t, n.hubDB, addr, otherPubKey)
	to := "HMC-ec00000000000011"
	tx := signedTransfer(t, n.signer, to, 0, V2ChainID)
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, tx); err == nil || code != "pubkey_binding_mismatch" {
		t.Fatalf("submit: code=%q err=%v", code, err)
	}
	h := queueTransfer(t, n.hubDB, tx)
	n.mine()
	var status, code string
	if err := n.hubDB.QueryRow(`SELECT status, reject_code FROM tx_history WHERE tx_hash=?`, h).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	if status != "rejected" || code != "pubkey_binding_mismatch" {
		t.Fatalf("apply: status=%q code=%q", status, code)
	}
	if b, _ := unitsOf(t, n.hubDB, to); b != 0 {
		t.Fatal("mismatched key moved funds")
	}
	if pub, _ := boundKey(t, n.hubDB, addr); pub != otherPubKey {
		t.Fatalf("binding replaced: %q", pub)
	}
}

// Before the activation height the binding is only recorded, never enforced (history replays
// exactly as before).
func TestPubKeyBindingNotEnforcedBeforeV2(t *testing.T) {
	n := newV2Net(t, "100")
	addr := n.signer.Address()
	fundAddr(t, n.hubDB, addr, HMCToUnits(1))
	preBind(t, n.hubDB, addr, otherPubKey)
	to := "HMC-ec00000000000012"
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, signedTransfer(t, n.signer, to, 0, "")); err != nil {
		t.Fatalf("pre-v2 submit: %s %v", code, err)
	}
	n.mine()
	if b, _ := unitsOf(t, n.hubDB, to); b != 1000 {
		t.Fatalf("pre-v2 transfer not applied: %d", b)
	}
}

func TestBindAddressPubKeyProof(t *testing.T) {
	n := newV2Net(t, "1")
	s, err := nodecrypto.LoadOrCreate(filepath.Join(t.TempDir(), "owner"))
	if err != nil {
		t.Fatal(err)
	}
	addr, pub := s.Address(), s.PublicKeyHex()
	sig := s.SignHex(AddressBindMessage(addr, pub))
	if code, _ := n.hub.BindAddressPubKey(n.ctx, addr, pub, sig); code != "unknown_address" {
		t.Fatalf("unfunded: %q", code)
	}
	fundAddr(t, n.hubDB, addr, 5)
	if code, _ := n.hub.BindAddressPubKey(n.ctx, addr, pub, strings.Repeat("00", ed25519.SignatureSize)); code != "invalid_signature" {
		t.Fatalf("bad sig: %q", code)
	}
	if code, _ := n.hub.BindAddressPubKey(n.ctx, "HMC-0000000000000000", pub, sig); code != "address_pubkey_mismatch" {
		t.Fatalf("foreign address: %q", code)
	}
	if code, err := n.hub.BindAddressPubKey(n.ctx, addr, pub, sig); err != nil || code != "bound" {
		t.Fatalf("bind: %q %v", code, err)
	}
	if code, err := n.hub.BindAddressPubKey(n.ctx, addr, pub, sig); err != nil || code != "already_bound" {
		t.Fatalf("rebind: %q %v", code, err)
	}
	if got, src := boundKey(t, n.hubDB, addr); got != pub || src != PubKeyBindSourceProof {
		t.Fatalf("stored %q/%q", got, src)
	}
	if got, _ := n.hub.BoundPubKey(n.ctx, addr); got != pub {
		t.Fatalf("BoundPubKey %q", got)
	}
}

// An address already bound to another key cannot be re-bound by a bind proof.
func TestBindAddressPubKeyRefusesRebindToOtherKey(t *testing.T) {
	n := newV2Net(t, "1")
	s, err := nodecrypto.LoadOrCreate(filepath.Join(t.TempDir(), "owner"))
	if err != nil {
		t.Fatal(err)
	}
	addr, pub := s.Address(), s.PublicKeyHex()
	fundAddr(t, n.hubDB, addr, 5)
	preBind(t, n.hubDB, addr, otherPubKey)
	if code, err := n.hub.BindAddressPubKey(n.ctx, addr, pub, s.SignHex(AddressBindMessage(addr, pub))); err == nil || code != "pubkey_binding_mismatch" {
		t.Fatalf("rebind: %q %v", code, err)
	}
}

// SUP spends record their key too (enforcement shares checkPubKeyBindingQ with HMC).
func TestPubKeyBindingRecordedForSUPSpend(t *testing.T) {
	ctx := context.Background()
	svc, addrA, privA, addrB := supTestWallet(t)
	pubA := hex.EncodeToString(privA.Public().(ed25519.PublicKey))
	tx := signSupTransfer(t, SupTransferTx{
		TxType: "transfer_sup_v1", From: addrA, To: addrB, AmountUnits: SUPToUnits(1.0),
		FeeUnits: DefaultSUPTransferMinFee, TimestampUnix: time.Now().Unix(), PubKeyEd25519: pubA,
	}, privA)
	if _, _, err := svc.SubmitSupTransferTx(ctx, tx); err != nil {
		t.Fatal(err)
	}
	m, _ := svc.PoHTargetMod(ctx)
	nn, e := firstPoHHit(m)
	if _, err := svc.AppendPoHBlock(ctx, "HMC-node", nn, e, 0, m, ""); err != nil {
		t.Fatal(err)
	}
	if got, src := boundKey(t, svc.db, addrA); got != pubA || src != PubKeyBindSourceSUP {
		t.Fatalf("sup binding %q/%q", got, src)
	}
}

// The migration seeds bindings from history once; the earliest spending key wins.
func TestAddressPubKeysBackfillEarliestWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bf.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	addr := "HMC-backfill000001"
	k1, k2 := strings.Repeat("aa", 32), strings.Repeat("bb", 32)
	for i, k := range []string{k2, k1} { // k1 at the lower block index
		raw, _ := json.Marshal(map[string]any{"pubkey_ed25519": k})
		if _, err := db.Exec(`INSERT INTO tx_history (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
			VALUES (?, ?, ?, 'HMC-x', ?, 0, 1, 'included', ?, 'h', 0, '')`, "h"+string(rune('a'+i)), string(raw), addr, i, 10-i*5); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`DELETE FROM meta WHERE key='address_pubkeys_backfill_v1'`); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	db, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got, src := boundKey(t, db, addr); got != k1 || src != "backfill_hmc" {
		t.Fatalf("backfill %q/%q", got, src)
	}
}

// Audit: the earliest spend across ledgers wins, and a malformed legacy tx_json row does not
// fail the migration (node start).
func TestAddressPubKeysBackfillAcrossLedgersAndMalformedRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bf2.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	addr := "HMC-backfill000002"
	kSup, kHmc := strings.Repeat("cc", 32), strings.Repeat("dd", 32)
	rawSup, _ := json.Marshal(map[string]any{"pubkey_ed25519": kSup})
	rawHmc, _ := json.Marshal(map[string]any{"pubkey_ed25519": kHmc})
	for _, q := range []struct {
		table, hash, raw string
		idx              int
	}{
		{"sup_tx_history", "s1", string(rawSup), 3},
		{"tx_history", "h1", string(rawHmc), 9},
		{"tx_history", "bad", "not json", 1},
	} {
		if _, err := db.Exec(`INSERT INTO `+q.table+` (tx_hash, tx_json, from_address, to_address, nonce, fee_units, amount_units, status, block_index, block_hash, applied_at, reject_code)
			VALUES (?, ?, ?, 'HMC-x', 0, 0, 1, 'included', ?, 'h', 0, '')`, q.hash, q.raw, addr, q.idx); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = db.Exec(`DELETE FROM meta WHERE key='address_pubkeys_backfill_v1'`)
	_, _ = db.Exec(`DELETE FROM address_pubkeys`)
	_ = db.Close()
	db, err = store.Open(path)
	if err != nil {
		t.Fatalf("migration must survive a malformed row: %v", err)
	}
	defer db.Close()
	if got, src := boundKey(t, db, addr); got != kSup || src != "backfill_sup" {
		t.Fatalf("earliest cross-ledger spend must win: %q/%q", got, src)
	}
}
