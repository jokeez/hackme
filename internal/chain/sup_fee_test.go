package chain

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
)

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
	svc, priv, pubHex, addr := fundedSUPHolder(t)
	ctx := t.Context()
	cid := svc.ChainID(ctx)
	b := signFee(svc, priv, pubHex, cid, addr, 3000, SUPFeeProductHMCOrder, "order-1")
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
	svc, priv, pubHex, addr := fundedSUPHolder(t)
	ctx := t.Context()
	cid := svc.ChainID(ctx)
	cases := []struct {
		name string
		b    SUPFeeBurn
		code string
	}{
		{"other chain_id", signFee(svc, priv, pubHex, "hackme-other", addr, 100, SUPFeeProductHMCOrder, "r1"), "invalid_signature"},
		{"unknown product", signFee(svc, priv, pubHex, cid, addr, 100, "free_lunch", "r2"), "unknown_product"},
		{"over balance", signFee(svc, priv, pubHex, cid, addr, 10_001, SUPFeeProductHMCOrder, "r3"), "insufficient_sup_balance"},
		{"bad ref", signFee(svc, priv, pubHex, cid, addr, 100, SUPFeeProductHMCOrder, "a|b"), "invalid_ref"},
		{"zero", signFee(svc, priv, pubHex, cid, addr, 0, SUPFeeProductHMCOrder, "r4"), "invalid_amount"},
	}
	for _, c := range cases {
		if code, err := svc.BurnSUPForFee(ctx, c.b); code != c.code || err == nil {
			t.Fatalf("%s: code=%q err=%v want %q", c.name, code, err, c.code)
		}
	}
	// Someone else's key cannot spend addr's SUP.
	_, priv2, _ := ed25519.GenerateKey(rand.Reader)
	stolen := signFee(svc, priv2, pubHex, cid, addr, 100, SUPFeeProductHMCOrder, "r5")
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
