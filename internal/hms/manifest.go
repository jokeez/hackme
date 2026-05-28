package hms

import (
	"crypto/sha256"
	"encoding/binary"
)

// LeafHash implements SPEC: SHA256(chunk_id || ciphertext_sha256 || size || erasure_meta).
func LeafHash(chunkID string, ciphertextSHA256 []byte, size uint64, erasureMeta []byte) [32]byte {
	h := sha256.New()
	_, _ = h.Write([]byte(chunkID))
	if len(ciphertextSHA256) == 32 {
		_, _ = h.Write(ciphertextSHA256)
	} else {
		sum := sha256.Sum256(ciphertextSHA256)
		_, _ = h.Write(sum[:])
	}
	var sz [8]byte
	binary.BigEndian.PutUint64(sz[:], size)
	_, _ = h.Write(sz[:])
	if len(erasureMeta) > 0 {
		_, _ = h.Write(erasureMeta)
	}
	return sha256.Sum256(h.Sum(nil))
}

// MerkleRoot builds a binary Merkle tree over leaf hashes (duplicate last when odd).
func MerkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	level := append([][32]byte(nil), leaves...)
	for len(level) > 1 {
		var next [][32]byte
		for i := 0; i < len(level); i += 2 {
			left := level[i]
			right := left
			if i+1 < len(level) {
				right = level[i+1]
			}
			next = append(next, pairHash(left, right))
		}
		level = next
	}
	return level[0]
}

func pairHash(a, b [32]byte) [32]byte {
	h := sha256.New()
	_, _ = h.Write(a[:])
	_, _ = h.Write(b[:])
	return sha256.Sum256(h.Sum(nil))
}
