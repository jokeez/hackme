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
	"runtime"
)

const seedFile = "node_ed25519.seed"

// Signer holds an Ed25519 keypair for non-repudiation of API payloads (e.g. order manifests).
type Signer struct {
	priv ed25519.PrivateKey
	pub  ed25519.PublicKey
}

// ensureSeedFileMode tightens seed files to 0600 when group/other bits are set.
// Common after zip/deb extract, Windows NTFS (reports 0666), Docker volume copy, or umask.
// Returns the refreshed FileInfo after a successful repair, or an error if still too open.
func ensureSeedFileMode(path string, fi os.FileInfo) (os.FileInfo, error) {
	perm := fi.Mode().Perm()
	if perm&0o077 == 0 {
		return fi, nil
	}
	// Best-effort repair (owner can chmod own file on Unix; Windows Chmod is limited).
	_ = os.Chmod(path, 0o600)
	fi2, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	perm2 := fi2.Mode().Perm()
	if perm2&0o077 == 0 {
		return fi2, nil
	}
	// Windows FileMode.Perm() typically stays 0666 even after Chmod — ACLs gate access.
	// Do not block mining/wallet on that artifact; refuse only on Unix where bits are real.
	if runtime.GOOS == "windows" {
		return fi2, nil
	}
	return nil, fmt.Errorf("nodecrypto: seed file permissions too open (%04o; need 0600): %s", perm2, path)
}

// ReadSeedFile reads a seed file after rejecting symlinks and repairing group/other-readable modes (H52).
// Same policy as operator openSecretFile (H51), plus auto-chmod 0600 so installers do not brick mining.
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
	if _, err := ensureSeedFileMode(path, fi); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// LoadOrCreate loads a 32-byte seed from dataDir/node_ed25519.seed or generates one.
func LoadOrCreate(dataDir string) (*Signer, error) {
	if dataDir == "" {
		return nil, errors.New("nodecrypto: empty dataDir")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, err
	}
	_ = os.Chmod(dataDir, 0o700)
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
		_ = os.Chmod(path, 0o600)
		seed = []byte(hex.EncodeToString(s))
	}
	raw := make([]byte, ed25519.SeedSize)
	n, err := hex.Decode(raw, seed)
	if err != nil {
		return nil, fmt.Errorf("nodecrypto: corrupt seed file %s: %w", path, err)
	}
	if n != ed25519.SeedSize {
		return nil, fmt.Errorf("nodecrypto: seed file %s must be %d hex bytes (got %d decoded)", path, ed25519.SeedSize, n)
	}
	priv := ed25519.NewKeyFromSeed(raw)
	return &Signer{priv: priv, pub: priv.Public().(ed25519.PublicKey)}, nil
}

// Load reads dataDir/node_ed25519.seed and never creates one. Tools that sign on behalf of an
// existing node (e.g. checkpoints) must fail on a wrong path instead of minting a new identity.
func Load(dataDir string) (*Signer, error) {
	if dataDir == "" {
		return nil, errors.New("nodecrypto: empty dataDir")
	}
	path := filepath.Join(dataDir, seedFile)
	seed, err := ReadSeedFile(path)
	if err != nil {
		return nil, err
	}
	raw := make([]byte, ed25519.SeedSize)
	n, err := hex.Decode(raw, seed)
	if err != nil {
		return nil, fmt.Errorf("nodecrypto: corrupt seed file %s: %w", path, err)
	}
	if n != ed25519.SeedSize {
		return nil, fmt.Errorf("nodecrypto: seed file %s must be %d hex bytes (got %d decoded)", path, ed25519.SeedSize, n)
	}
	priv := ed25519.NewKeyFromSeed(raw)
	return &Signer{priv: priv, pub: priv.Public().(ed25519.PublicKey)}, nil
}

// AddressForPublicKey derives the HMC- address for a raw Ed25519 public key (same rule as Address).
func AddressForPublicKey(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return "HMC-" + hex.EncodeToString(sum[:])[:16]
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

// LongAddress returns the Economics v2 long address: HMC- + all 64 hex of sha256(pubkey).
// Same key as Address(); valid on chain from the v2 activation height.
func (s *Signer) LongAddress() string {
	sum := sha256.Sum256(s.pub)
	return "HMC-" + hex.EncodeToString(sum[:])
}

// Sign returns a detached Ed25519 signature (128 hex chars).
func (s *Signer) Sign(message []byte) []byte {
	return ed25519.Sign(s.priv, message)
}

// SignHex returns hex-encoded signature.
func (s *Signer) SignHex(message []byte) string {
	return hex.EncodeToString(s.Sign(message))
}
