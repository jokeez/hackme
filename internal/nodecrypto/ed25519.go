package nodecrypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const seedFile = "node_ed25519.seed"

// Signer holds an Ed25519 keypair for non-repudiation of API payloads (e.g. order manifests).
type Signer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

// ReadSeedFile reads a seed file after rejecting symlinks and group/other-readable modes (H52).
// Same policy as operator openSecretFile (H51).
func ReadSeedFile(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("nodecrypto: seed file is a symlink: %s", path)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("nodecrypto: seed file is not a regular file: %s", path)
	}
	if perm := fi.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("nodecrypto: seed file permissions too open (%04o; need 0600): %s", perm, path)
	}
	return os.ReadFile(path)
}

// LoadOrCreate loads a 32-byte seed from dataDir/node_ed25519.seed or generates one.
func LoadOrCreate(dataDir string) (*Signer, error) {
	if dataDir == "" {
		return nil, errors.New("nodecrypto: empty dataDir")
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dataDir, seedFile)
	seed, err := ReadSeedFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		s := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(s); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(hex.EncodeToString(s)), 0o600); err != nil {
			return nil, err
		}
		seed = []byte(hex.EncodeToString(s))
	}
	raw := make([]byte, ed25519.SeedSize)
	if _, err := hex.Decode(raw, seed); err != nil {
		return nil, fmt.Errorf("nodecrypto: corrupt seed file %s: %w", path, err)
	}
	priv := ed25519.NewKeyFromSeed(raw)
	return &Signer{priv: priv, pub: priv.Public().(ed25519.PublicKey)}, nil
}

// PublicKeyHex returns the public key as 64 hex chars (32 bytes).
func (s *Signer) PublicKeyHex() string {
	return hex.EncodeToString(s.pub)
}

// Address returns deterministic node/wallet address derived from signer public key.
// Format: HMC- + first 16 hex chars of sha256(pubkey).
func (s *Signer) Address() string {
	sum := sha256.Sum256(s.pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
}

// Sign returns a detached Ed25519 signature (128 hex chars).
func (s *Signer) Sign(message []byte) []byte {
	return ed25519.Sign(s.priv, message)
}

// SignHex returns hex-encoded signature.
func (s *Signer) SignHex(message []byte) string {
	return hex.EncodeToString(s.Sign(message))
}
