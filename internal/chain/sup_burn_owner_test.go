package chain

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"path/filepath"
	"testing"

	"hackme/internal/nodecrypto"
	"hackme/internal/store"
)

func TestBurnSUPForServiceRejectsForeignPayer(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "burn-owner.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	svc := New(db)
	wallet := "HMC-burnownerwallet1"
	if _, _, err := svc.InitGenesis(ctx, wallet); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatal(err)
	}
	victim := "HMC-victimwallet0001"
	if code, err := svc.MintSUP(ctx, victim, SUPToUnits(5), "fund"); err != nil || code != "" {
		t.Fatalf("mint victim: %v %s", err, code)
	}
	code, err := svc.BurnSUPForService(ctx, victim, SUPToUnits(1), "security_audit:x")
	if err == nil || code != "owner_required" {
		t.Fatalf("expected owner_required, got code=%q err=%v", code, err)
	}
	st, _ := svc.SupAddressState(ctx, victim)
	if st.BalanceSUPUnits != SUPToUnits(5) {
		t.Fatalf("victim balance changed: %d", st.BalanceSUPUnits)
	}
}

func TestBurnSUPWithOwnerProof(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "burn-proof.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	signer, err := nodecrypto.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	svc := NewWithSigner(db, signer)
	if _, _, err := svc.InitGenesis(ctx, signer.Address()); err != nil {
		t.Fatal(err)
	}
	if err := svc.InitSUPGenesis(ctx); err != nil {
		t.Fatal(err)
	}
	from := signer.Address()
	units := SUPToUnits(3)
	if code, err := svc.MintSUP(ctx, from, units, "fund"); err != nil || code != "" {
		t.Fatalf("mint: %v %s", err, code)
	}
	burnU := SUPToUnits(1)
	memo := "audit:proof"
	msg := BurnSUPCanonicalMessage(from, burnU, memo)
	sig := signer.SignHex(msg)
	code, err := svc.BurnSUPWithOwnerProof(ctx, from, burnU, memo, signer.PublicKeyHex(), sig)
	if err != nil || code != "" {
		t.Fatalf("proof burn: code=%q err=%v", code, err)
	}
	st, _ := svc.SupAddressState(ctx, from)
	if st.BalanceSUPUnits != units-burnU {
		t.Fatalf("balance=%d want %d", st.BalanceSUPUnits, units-burnU)
	}
	// Bad signature rejected.
	bad := hex.EncodeToString(make([]byte, ed25519.SignatureSize))
	if code, err := svc.BurnSUPWithOwnerProof(ctx, from, burnU, memo, signer.PublicKeyHex(), bad); err == nil || code == "" {
		t.Fatalf("expected bad sig reject, got code=%q err=%v", code, err)
	}
}
