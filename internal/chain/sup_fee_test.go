package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"

	"hackme/internal/fuzzescrow"
)

const testPublicSUPProduct = "test_public_product"

// withPublicTestProduct registers a public fee product for the holder-signed path (tests only).
func withPublicTestProduct(t *testing.T) {
	t.Helper()
	supFeeProducts[testPublicSUPProduct] = supFeeProductSpec{Public: true}
	t.Cleanup(func() { delete(supFeeProducts, testPublicSUPProduct) })
}

func fundedSUPHolder(t *testing.T) (*Service, ed25519.PrivateKey, string, string) {
	t.Helper()
	svc, ctx, _ := newSUPTestChain(t, "")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubHex := hex.EncodeToString(pub)
	addr, err := addressFromPubKeyHex(pubHex)
	if err != nil {
		t.Fatal(err)
	}
	if code, err := svc.MintSUP(ctx, addr, 10_000, "fund"); err != nil {
		t.Fatalf("fund: %s %v", code, err)
	}
	return svc, priv, pubHex, addr
}

func signFee(svc *Service, priv ed25519.PrivateKey, pubHex, chainID, from string, units uint64, product, ref string) SUPFeeBurn {
	msg := BurnSUPForFeeMessage(chainID, from, units, product, ref)
	return SUPFeeBurn{From: from, AmountUnits: units, Product: product, Ref: ref,
		PubKeyEd25519: pubHex, SigEd25519: hex.EncodeToString(ed25519.Sign(priv, msg))}
}

func TestBurnSUPForFeeOncePerProductRef(t *testing.T) {
	withPublicTestProduct(t)
	svc, priv, pubHex, addr := fundedSUPHolder(t)
	ctx := t.Context()
	cid := svc.ChainID(ctx)
	b := signFee(svc, priv, pubHex, cid, addr, 3000, testPublicSUPProduct, "order-1")
	if code, err := svc.BurnSUPForFee(ctx, b); err != nil {
		t.Fatalf("burn: %s %v", code, err)
	}
	// Replaying the same signed request (or a retry) never charges twice.
	if code, err := svc.BurnSUPForFee(ctx, b); code != "fee_ref_already_paid" || !errors.Is(err, ErrSUPFeeRefPaid) {
		t.Fatalf("replay: code=%q err=%v", code, err)
	}
	st, _ := svc.SupAddressState(ctx, addr)
	if st.BalanceSUPUnits != 7000 {
		t.Fatalf("balance %d want 7000", st.BalanceSUPUnits)
	}
	ec, _ := svc.SUPEconomics(ctx)
	if ec.TotalBurnedSUP != UnitsToSUP(3000) {
		t.Fatalf("total burned %v", ec.TotalBurnedSUP)
	}
}

func TestBurnSUPForFeeRejects(t *testing.T) {
	withPublicTestProduct(t)
	svc, priv, pubHex, addr := fundedSUPHolder(t)
	ctx := t.Context()
	cid := svc.ChainID(ctx)
	cases := []struct {
		name string
		b    SUPFeeBurn
		code string
	}{
		{"other chain_id", signFee(svc, priv, pubHex, "hackme-other", addr, 100, testPublicSUPProduct, "r1"), "invalid_signature"},
		{"unknown product", signFee(svc, priv, pubHex, cid, addr, 100, "free_lunch", "r2"), "unknown_product"},
		{"over balance", signFee(svc, priv, pubHex, cid, addr, 10_001, testPublicSUPProduct, "r3"), "insufficient_sup_balance"},
		{"bad ref", signFee(svc, priv, pubHex, cid, addr, 100, testPublicSUPProduct, "a|b"), "invalid_ref"},
		{"zero", signFee(svc, priv, pubHex, cid, addr, 0, testPublicSUPProduct, "r4"), "invalid_amount"},
		{"internal product", signFee(svc, priv, pubHex, cid, addr, 100, SUPFeeProductHMCOrder, "r6"), "product_internal_only"},
	}
	for _, c := range cases {
		if code, err := svc.BurnSUPForFee(ctx, c.b); code != c.code || err == nil {
			t.Fatalf("%s: code=%q err=%v want %q", c.name, code, err, c.code)
		}
	}
	// Someone else's key cannot spend addr's SUP.
	_, priv2, _ := ed25519.GenerateKey(rand.Reader)
	stolen := signFee(svc, priv2, pubHex, cid, addr, 100, testPublicSUPProduct, "r5")
	if code, _ := svc.BurnSUPForFee(ctx, stolen); code != "invalid_signature" {
		t.Fatalf("foreign key: %q", code)
	}
	if st, _ := svc.SupAddressState(ctx, addr); st.BalanceSUPUnits != 10_000 {
		t.Fatalf("rejected burns moved balance: %d", st.BalanceSUPUnits)
	}
}

// The v1 owner-proof message has no nonce; a seen signature must not burn the owner's SUP again.
func TestBurnSUPWithOwnerProofNoReplay(t *testing.T) {
	svc, priv, pubHex, addr := fundedSUPHolder(t)
	ctx := t.Context()
	sig := hex.EncodeToString(ed25519.Sign(priv, BurnSUPCanonicalMessage(addr, 500, "audit")))
	if code, err := svc.BurnSUPWithOwnerProof(ctx, addr, 500, "audit", pubHex, sig); err != nil {
		t.Fatalf("first: %s %v", code, err)
	}
	if code, err := svc.BurnSUPWithOwnerProof(ctx, addr, 500, "audit", pubHex, sig); code != "burn_replay" || err == nil {
		t.Fatalf("replay: code=%q err=%v", code, err)
	}
	if st, _ := svc.SupAddressState(ctx, addr); st.BalanceSUPUnits != 9500 {
		t.Fatalf("balance %d want 9500", st.BalanceSUPUnits)
	}
}

// Squat: someone burns their own SUP under a victim's predictable ref (order:/campaign:<id>)
// to make the victim's SUP-fee open fail with fee_ref_already_paid. hmc_order has no public
// path, and refs are scoped per payer, so neither works.
func TestSUPFeeRefSquatCannotBlockOpen(t *testing.T) {
	withPublicTestProduct(t)
	c := newFeeTestChain(t, "1", 200, HMCToUnits(20))
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pubHex := hex.EncodeToString(pub)
	attacker, _ := addressFromPubKeyHex(pubHex)
	if _, err := c.db.Exec(`INSERT INTO accounts(address, balance_units, balance_sup_units, next_nonce, sup_next_nonce, updated_at) VALUES(?,0,?,0,0,0)`, attacker, HMCToUnits(5)); err != nil {
		t.Fatal(err)
	}
	cid := c.svc.ChainID(c.ctx)
	squat := signFee(c.svc, priv, pubHex, cid, attacker, 1, SUPFeeProductHMCOrder, "campaign:dig-victim")
	if code, err := c.svc.BurnSUPForFee(c.ctx, squat); code != "product_internal_only" || !errors.Is(err, ErrSUPFeeInternal) {
		t.Fatalf("public hmc_order burn: code=%q err=%v", code, err)
	}
	// Even on a public product the attacker's burn sits under its own payer, not the victim's.
	pubSquat := signFee(c.svc, priv, pubHex, cid, attacker, 1, testPublicSUPProduct, "campaign:dig-victim")
	if code, err := c.svc.BurnSUPForFee(c.ctx, pubSquat); err != nil {
		t.Fatalf("attacker own burn: %s %v", code, err)
	}
	c.svc.mu.Lock()
	tx, err := c.db.BeginTx(c.ctx, nil)
	if err != nil {
		c.svc.mu.Unlock()
		t.Fatal(err)
	}
	code, err := c.svc.burnSUPForFeeTx(c.ctx, tx, c.wallet, 10, testPublicSUPProduct, "campaign:dig-victim")
	_ = tx.Rollback()
	c.svc.mu.Unlock()
	if err != nil {
		t.Fatalf("victim burn under same ref blocked: %s %v", code, err)
	}
	// The real open pays its SUP fee normally.
	if _, err := c.svc.OpenFuzzEscrowWithFee(c.ctx, "dig-victim", 100, 100, fuzzescrow.EscrowSplit2080, "SUP"); err != nil {
		t.Fatalf("victim open after squat attempt: %v", err)
	}
}
