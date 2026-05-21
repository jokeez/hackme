package worksubmit

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestSignPayload_TamperOneByteInvalidatesSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	p := SignPayload{
		WorkerID:    "worker-fuzz",
		BaseNonce:   100,
		BatchSize:   1000,
		WorkID:      "worker-fuzz:100:1000",
		Attempts:    1000,
		SubmitNonce: 3,
	}
	sig := ed25519.Sign(priv, p.CanonicalJSON())
	raw := p.CanonicalJSON()
	// Flip one byte in canonical JSON (simulates MITM bit-flip on result log).
	if len(raw) == 0 {
		t.Fatal("empty canonical json")
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-1] ^= 0x01
	if ed25519.Verify(pub, tampered, sig) {
		t.Fatal("signature must not verify after one-byte tamper")
	}
	// Field-level tamper after sign.
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["attempts"].(float64); ok {
		m["attempts"] = v + 1
	}
	tampered2, _ := json.Marshal(m)
	if ed25519.Verify(pub, tampered2, sig) {
		t.Fatal("signature must not verify when attempts field changes")
	}
	_ = hex.EncodeToString(sig)
}
