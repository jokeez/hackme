package nodecrypto

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadOrCreateSign(t *testing.T) {
	dir := t.TempDir()
	s1, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	msg := []byte(`{"id":"x","reward_hmc":0.01}`)
	sig := s1.SignHex(msg)
	if len(sig) != 128 {
		t.Fatalf("sig len %d", len(sig))
	}
	s2, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s2.PublicKeyHex() != s1.PublicKeyHex() {
		t.Fatal("pub mismatch")
	}
	if s2.SignHex(msg) != sig {
		t.Fatal("deterministic sign")
	}
	if s1.Address() != s2.Address() {
		t.Fatal("address mismatch")
	}
	if !strings.HasPrefix(s1.Address(), "HMC-") || len(s1.Address()) != 20 {
		t.Fatalf("unexpected address format: %q", s1.Address())
	}
	_ = filepath.Join(dir, seedFile)
}
