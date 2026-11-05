package chain

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hackme/internal/nodecrypto"
)

func TestAddressFormats(t *testing.T) {
	legacy := "HMC-0123456789abcdef"
	long := "HMC-" + strings.Repeat("ab", 32)
	for _, c := range []struct {
		a            string
		legacy, long bool
	}{
		{legacy, true, false},
		{long, false, true},
		{"HMC-0123456789ABCDEF", false, false},            // uppercase is a different, unspendable string
		{"HMC-" + strings.Repeat("AB", 32), false, false}, // same
		{"HMC-0123456789abcde", false, false},             // 15 hex
		{"HMC-" + strings.Repeat("a", 63), false, false},
		{"HMC-" + strings.Repeat("a", 128), false, false}, // 128 hex is not a format
		{"hmc-0123456789abcdef", false, false},
		{"0123456789abcdef", false, false},
		{"HMC-0123456789abcdeg", false, false},
		{"HMC-receiver000001", false, false},
		{"", false, false},
	} {
		if IsLegacyAddress(c.a) != c.legacy || IsLongAddress(c.a) != c.long || IsValidAddress(c.a) != (c.legacy || c.long) {
			t.Errorf("%q: legacy=%v long=%v", c.a, IsLegacyAddress(c.a), IsLongAddress(c.a))
		}
	}
}

func TestLongAddressDerivation(t *testing.T) {
	s, err := nodecrypto.LoadOrCreate(filepath.Join(t.TempDir(), "k"))
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := hex.DecodeString(s.PublicKeyHex())
	sum := sha256.Sum256(pub)
	want := "HMC-" + hex.EncodeToString(sum[:])
	got, err := LongAddressFromPubKeyHex(s.PublicKeyHex())
	if err != nil || got != want || s.LongAddress() != want || !IsLongAddress(want) {
		t.Fatalf("long address %q / signer %q, want %q", got, s.LongAddress(), want)
	}
	// The legacy address is the prefix of the long one: same key, two formats.
	if !strings.HasPrefix(want, s.Address()) || !IsLegacyAddress(s.Address()) {
		t.Fatalf("legacy %q is not the prefix of %q", s.Address(), want)
	}
	if !pubKeyOwnsAddress(s.PublicKeyHex(), s.Address()) || !pubKeyOwnsAddress(s.PublicKeyHex(), want) {
		t.Fatal("key must own both of its addresses")
	}
	if pubKeyOwnsAddress(s.PublicKeyHex(), want[:len(want)-1]+"0") || pubKeyOwnsAddress(s.PublicKeyHex(), strings.ToUpper(want)) {
		t.Fatal("near-miss address accepted")
	}
	if _, err := LongAddressFromPubKeyHex("zz"); err == nil {
		t.Fatal("bad pubkey accepted")
	}
}

func signedTransferFrom(t *testing.T, s *nodecrypto.Signer, from, to string, nonce uint64, chainID string) TransferTx {
	t.Helper()
	tx := TransferTx{
		TxType: "transfer_v1", From: from, To: to, AmountUnits: 1000, FeeUnits: DefaultTransferMinFee,
		Nonce: nonce, TimestampUnix: time.Now().Unix(), Memo: "t", PubKeyEd25519: s.PublicKeyHex(), ChainID: chainID,
	}
	canon, err := tx.CanonicalBytes()
	if err != nil {
		t.Fatal(err)
	}
	tx.SigEd25519 = s.SignHex(canon)
	return tx
}

func txHistoryStatus(t *testing.T, n *v2Net, hash string) (status, code string) {
	t.Helper()
	if err := n.hubDB.QueryRow(`SELECT status, reject_code FROM tx_history WHERE tx_hash=?`, hash).Scan(&status, &code); err != nil {
		t.Fatal(err)
	}
	return
}

// Before the height a long address is neither a recipient nor a sender — at submit and, for a
// transfer already queued, at block apply. Legacy transfers are unchanged.
func TestLongAddressRejectedBeforeV2(t *testing.T) {
	n := newV2Net(t, "100")
	legacy, long := n.signer.Address(), n.signer.LongAddress()
	fundAddr(t, n.hubDB, legacy, HMCToUnits(1))
	fundAddr(t, n.hubDB, long, HMCToUnits(1))
	toLong := signedTransferFrom(t, n.signer, legacy, "HMC-"+strings.Repeat("cd", 32), 0, "")
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, toLong); err == nil || code != "address_format_not_active" {
		t.Fatalf("to long before v2: %q %v", code, err)
	}
	fromLong := signedTransferFrom(t, n.signer, long, "HMC-ec00000000000020", 0, "")
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, fromLong); err == nil || code != "address_format_not_active" {
		t.Fatalf("from long before v2: %q %v", code, err)
	}
	h := queueTransfer(t, n.hubDB, toLong)
	n.mine()
	if st, code := txHistoryStatus(t, n, h); st != "rejected" || code != "address_format_not_active" {
		t.Fatalf("queued to-long applied: %s %s", st, code)
	}
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, signedTransferFrom(t, n.signer, legacy, "HMC-ec00000000000021", 0, "")); err != nil {
		t.Fatalf("legacy before v2: %s %v", code, err)
	}
}

// From the height: an owner moves funds legacy → long (same key, ordinary transfer), then spends
// from the long address. Nothing migrates on its own.
func TestLongAddressMigrationAndSpendFromV2(t *testing.T) {
	n := newV2Net(t, "1")
	legacy, long := n.signer.Address(), n.signer.LongAddress()
	fundAddr(t, n.hubDB, legacy, HMCToUnits(1))
	if b, _ := unitsOf(t, n.hubDB, long); b != 0 {
		t.Fatal("long address funded without a transfer")
	}
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, signedTransferFrom(t, n.signer, legacy, long, 0, V2ChainID)); err != nil {
		t.Fatalf("legacy → own long: %s %v", code, err)
	}
	n.mine()
	if b, _ := unitsOf(t, n.hubDB, long); b != 1000 {
		t.Fatalf("long balance after migration %d", b)
	}
	to := "HMC-" + strings.Repeat("ef", 32)
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, signedTransferFrom(t, n.signer, long, to, 0, V2ChainID)); code != "insufficient_balance" {
		// 1000 units cannot also cover amount+fee: the long account is a real, separate account.
		t.Fatalf("spend more than the long balance: %s %v", code, err)
	}
	fundAddr(t, n.hubDB, long, HMCToUnits(1))
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, signedTransferFrom(t, n.signer, long, to, 0, V2ChainID)); err != nil {
		t.Fatalf("spend from long: %s %v", code, err)
	}
	n.mine()
	if b, _ := unitsOf(t, n.hubDB, to); b != 1000 {
		t.Fatalf("long → long recipient %d", b)
	}
	if code, _ := n.hub.BindAddressPubKey(n.ctx, long, n.signer.PublicKeyHex(), n.signer.SignHex(AddressBindMessage(long, n.signer.PublicKeyHex()))); code != "not_needed" {
		t.Fatalf("bind long: %q", code)
	}
}

// A long address belongs to exactly one key: another key cannot spend it, and malformed or
// uppercase recipients are refused after the height (they would strand funds).
func TestLongAddressOwnershipAndRecipientFormat(t *testing.T) {
	n := newV2Net(t, "1")
	victim, err := nodecrypto.LoadOrCreate(filepath.Join(t.TempDir(), "victim"))
	if err != nil {
		t.Fatal(err)
	}
	fundAddr(t, n.hubDB, victim.LongAddress(), HMCToUnits(1))
	steal := signedTransferFrom(t, n.signer, victim.LongAddress(), "HMC-ec00000000000030", 0, V2ChainID)
	if _, code, _ := n.hub.SubmitTransferTx(n.ctx, steal); code != "address_pubkey_mismatch" {
		t.Fatalf("other key spends long: %q", code)
	}
	fundAddr(t, n.hubDB, n.signer.Address(), HMCToUnits(1))
	for _, bad := range []string{
		"HMC-EC00000000000031",
		"HMC-" + strings.ToUpper(strings.Repeat("ab", 32)),
		"HMC-ec000000000031",
		"HMC-receiver000031",
		"HMC-" + strings.Repeat("ab", 64),
	} {
		if _, code, _ := n.hub.SubmitTransferTx(n.ctx, signedTransferFrom(t, n.signer, n.signer.Address(), bad, 0, V2ChainID)); code != "invalid_address" {
			t.Errorf("recipient %q: code %q", bad, code)
		}
	}
	// Queued malformed recipient is rejected at apply too.
	h := queueTransfer(t, n.hubDB, signedTransferFrom(t, n.signer, n.signer.Address(), "HMC-EC00000000000032", 0, V2ChainID))
	n.mine()
	if st, code := txHistoryStatus(t, n, h); st != "rejected" || code != "invalid_address" {
		t.Fatalf("queued bad recipient: %s %s", st, code)
	}
}

// Boundary: the block before the height rejects long addresses, the first v2 block accepts them.
func TestLongAddressActivationBoundary(t *testing.T) {
	n := newV2Net(t, "3")
	fundAddr(t, n.hubDB, n.signer.Address(), HMCToUnits(1))
	long := n.signer.LongAddress()
	n.mine() // block 1; next is 2 (v1)
	if _, code, _ := n.hub.SubmitTransferTx(n.ctx, signedTransferFrom(t, n.signer, n.signer.Address(), long, 0, "")); code != "address_format_not_active" {
		t.Fatalf("next block 2: %q", code)
	}
	n.mine() // block 2; next is 3 (v2)
	if _, code, err := n.hub.SubmitTransferTx(n.ctx, signedTransferFrom(t, n.signer, n.signer.Address(), long, 0, V2ChainID)); err != nil {
		t.Fatalf("next block 3: %s %v", code, err)
	}
	n.mine()
	if b, _ := unitsOf(t, n.hubDB, long); b != 1000 {
		t.Fatalf("long credited %d", b)
	}
}

// SUP and HMS take the same address rules (shared helpers).
func TestLongAddressSUPAndHMS(t *testing.T) {
	n := newV2Net(t, "1")
	long := n.signer.LongAddress()
	if _, err := n.hubDB.Exec(`INSERT INTO accounts(address, balance_units, balance_sup_units, balance_hms_units, next_nonce, sup_next_nonce, hms_next_nonce, updated_at)
		VALUES(?, 0, ?, ?, 0, 0, 0, 0)`, long, SUPToUnits(5), uint64(5_000_000_000)); err != nil {
		t.Fatal(err)
	}
	sup := SupTransferTx{TxType: "transfer_sup_v1", From: long, To: "HMC-ec00000000000040", AmountUnits: SUPToUnits(1),
		FeeUnits: DefaultSUPTransferMinFee, TimestampUnix: time.Now().Unix(), PubKeyEd25519: n.signer.PublicKeyHex(), ChainID: V2ChainID}
	b, _ := sup.CanonicalBytes()
	sup.SigEd25519 = n.signer.SignHex(b)
	if _, code, err := n.hub.SubmitSupTransferTx(n.ctx, sup); err != nil {
		t.Fatalf("sup from long: %s %v", code, err)
	}
	bad := sup
	bad.To = "HMC-EC00000000000040"
	b, _ = bad.CanonicalBytes()
	bad.SigEd25519 = n.signer.SignHex(b)
	if _, code, _ := n.hub.SubmitSupTransferTx(n.ctx, bad); code != "invalid_address" && code != "pending_nonce_conflict" {
		t.Fatalf("sup bad recipient: %q", code)
	}
	hms := HmsTransferTx{TxType: "transfer_hms_v1", From: long, To: "HMC-ec00000000000041", AmountUnits: 1_000_000,
		FeeUnits: DefaultHMSTransferMinFee, TimestampUnix: time.Now().Unix(), PubKeyEd25519: n.signer.PublicKeyHex(), ChainID: V2ChainID}
	b, _ = hms.CanonicalBytes()
	hms.SigEd25519 = n.signer.SignHex(b)
	if _, code, err := n.hub.SubmitHmsTransferTx(n.ctx, hms); err != nil {
		t.Fatalf("hms from long: %s %v", code, err)
	}
	other, _ := nodecrypto.LoadOrCreate(filepath.Join(t.TempDir(), "other"))
	steal := hms
	steal.PubKeyEd25519 = other.PublicKeyHex()
	steal.Nonce = 1
	b, _ = steal.CanonicalBytes()
	steal.SigEd25519 = other.SignHex(b)
	if _, code, _ := n.hub.SubmitHmsTransferTx(n.ctx, steal); code != "address_pubkey_mismatch" {
		t.Fatalf("hms other key: %q", code)
	}
	n.mine()
	if _, s2 := unitsOf(t, n.hubDB, "HMC-ec00000000000040"); s2 != SUPToUnits(1) {
		t.Fatalf("sup recipient %d", s2)
	}
}
