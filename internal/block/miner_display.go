package block

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// EffectiveMinerAddress returns miner_address when stored; otherwise derives the
// same HMC-* label as nodecrypto.Signer.Address from miner_pubkey_ed25519 (PoH blocks).
func (b *Block) EffectiveMinerAddress() string {
	if b == nil {
		return ""
	}
	if a := strings.TrimSpace(b.MinerAddress); a != "" {
		return a
	}
	pubHex := strings.TrimSpace(b.MinerPubKey)
	if pubHex == "" {
		return ""
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return ""
	}
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}
