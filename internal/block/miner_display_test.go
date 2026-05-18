package block

import (
	"encoding/hex"
	"testing"

	"hackme/internal/nodecrypto"
)

func TestEffectiveMinerAddress_FromPubkeyWhenMinerEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := nodecrypto.LoadOrCreate(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := s.Address()
	b := &Block{
		MinerAddress: "",
		MinerPubKey:  s.PublicKeyHex(),
	}
	if got := b.EffectiveMinerAddress(); got != want {
		t.Fatalf("EffectiveMinerAddress: want %q got %q", want, got)
	}
}

func TestEffectiveMinerAddress_PrefersStoredMiner(t *testing.T) {
	b := &Block{
		MinerAddress: "HMC-aaaaaaaaaaaaaaaa",
		MinerPubKey:  hex.EncodeToString(make([]byte, 32)),
	}
	if got := b.EffectiveMinerAddress(); got != "HMC-aaaaaaaaaaaaaaaa" {
		t.Fatalf("want stored miner, got %q", got)
	}
}
