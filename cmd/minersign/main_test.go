package main

import (
	"crypto/ed25519"
	"strings"
	"testing"

	"hackme/internal/worksubmit"
)

func TestMinerSignCanonicalVerifies(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := worksubmit.SignPayload{
		WorkerID:    "w1",
		BaseNonce:   1,
		BatchSize:   100,
		WorkID:      "w1:1+100",
		Attempts:    100,
		Found:       false,
		FoundNonce:  0,
		ResultHash:  "",
		ProofHash:   "",
		SubmitNonce: 9,
	}
	msg := p.CanonicalJSON()
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(pub, msg, sig) {
		t.Fatal("verify failed")
	}
	if strings.Contains(string(msg), " ") {
		t.Fatalf("unexpected whitespace in canonical JSON: %q", msg)
	}
}
