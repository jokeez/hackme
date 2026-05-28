package hms

import (
	"crypto/sha256"
	"encoding/binary"
)

// ProofBinding hashes challenge parameters (coordinator + worker both use this).
func ProofBinding(epochID int64, workerID, chunkID string, sectorOffset uint64, ciphertextSHA256 []byte) [32]byte {
	h := sha256.New()
	var e [8]byte
	binary.BigEndian.PutUint64(e[:], uint64(epochID))
	_, _ = h.Write(e[:])
	_, _ = h.Write([]byte(workerID))
	_, _ = h.Write([]byte(chunkID))
	binary.Write(h, binary.BigEndian, sectorOffset)
	if len(ciphertextSHA256) == 32 {
		_, _ = h.Write(ciphertextSHA256)
	} else {
		sum := sha256.Sum256(ciphertextSHA256)
		_, _ = h.Write(sum[:])
	}
	return sha256.Sum256(h.Sum(nil))
}

// ExpectedProofHash = SHA256(binding || sector_proof) where sector_proof = SHA256(32 bytes at offset).
func ExpectedProofHash(binding [32]byte, sectorProof [32]byte) [32]byte {
	h := sha256.New()
	_, _ = h.Write(binding[:])
	_, _ = h.Write(sectorProof[:])
	return sha256.Sum256(h.Sum(nil))
}

// SectorProofFromCiphertext returns SHA256(32-byte sector at offset) from local ciphertext.
func SectorProofFromCiphertext(ciphertext []byte, offset uint64) [32]byte {
	if len(ciphertext) == 0 {
		return [32]byte{}
	}
	start := int(offset)
	if start >= len(ciphertext) {
		start = 0
	}
	end := start + 32
	if end > len(ciphertext) {
		end = len(ciphertext)
	}
	seg := make([]byte, 32)
	copy(seg, ciphertext[start:end])
	return sha256.Sum256(seg)
}
