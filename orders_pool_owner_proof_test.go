package main

import (
	"testing"

	"hackme/internal/chain"
	"hackme/internal/nodecrypto"
)

func TestOrdersPoolOwnerProofRoundTrip(t *testing.T) {
	dir := t.TempDir()
	signer, err := nodecrypto.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{signer: signer}
	owner := chain.OrdersPrepaidNormalizeAddr(signer.Address())
	pub, sig, ts, err := a.signOrdersPoolOwnerProof(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyOrdersPoolOwnerProof(owner, pub, sig, ts); err != nil {
		t.Fatal(err)
	}
	if err := verifyOrdersPoolOwnerProof(owner, pub, sig, ts-ordersPoolOwnerProofTTLSec-10); err == nil {
		t.Fatal("expected expired")
	}
	if err := verifyOrdersPoolOwnerProof("HMC-0000000000000001", pub, sig, ts); err == nil {
		t.Fatal("expected pubkey mismatch")
	}
}
