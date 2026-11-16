package hms

import (
	"crypto/ed25519"
	"encoding/hex"
	"testing"
)

func TestVerifyRegisterWorkerPoP(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := RegisterWorkerPayload{
		WorkerID:    "w-pop",
		PubkeyHex:   hex.EncodeToString(pub),
		QuotaGB:     50,
		EndpointURL: "https://worker.example/push",
	}
	body, err := canonicalJSON(RegisterWorkerPayload{
		WorkerID:    "w-pop",
		PubkeyHex:   hex.EncodeToString(pub),
		QuotaGB:     50,
		EndpointURL: "https://worker.example/push",
	})
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, body)
	if err := VerifyRegisterWorker(p, hex.EncodeToString(sig)); err != nil {
		t.Fatalf("valid PoP: %v", err)
	}
	if err := VerifyRegisterWorker(p, ""); err == nil {
		t.Fatal("empty sig must fail")
	}
	if err := VerifyRegisterWorker(p, hex.EncodeToString(make([]byte, 64))); err == nil {
		t.Fatal("wrong sig must fail")
	}
}
