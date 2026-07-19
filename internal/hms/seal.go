package hms

import (
	"crypto/sha256"
	"encoding/binary"
	"math/big"
)

// SealHash implements SPEC: SHA256(epoch_id || manifest_root || pool_id || nonce).
func SealHash(epochID int64, manifestRoot [32]byte, poolID string, nonce uint64) [32]byte {
	h := sha256.New()
	var e [8]byte
	binary.BigEndian.PutUint64(e[:], uint64(epochID))
	_, _ = h.Write(e[:])
	_, _ = h.Write(manifestRoot[:])
	_, _ = h.Write([]byte(poolID))
	_ = binary.Write(h, binary.BigEndian, nonce)
	sum := h.Sum(nil)
	var out [32]byte
	copy(out[:], sum)
	return out
}

// HashBelowTarget returns true when hash < target (256-bit big-endian integers).
func HashBelowTarget(hash, target []byte) bool {
	if len(hash) != 32 || len(target) != 32 {
		return false
	}
	for i := 0; i < 32; i++ {
		if hash[i] < target[i] {
			return true
		}
		if hash[i] > target[i] {
			return false
		}
	}
	return false
}

// RetargetSeal adjusts difficulty from observed seal time (clamped).
func RetargetSeal(oldTarget []byte, actualSec, desiredSec int, clamp float64) []byte {
	out := make([]byte, 32)
	copy(out, oldTarget)
	if desiredSec <= 0 || actualSec <= 0 || len(oldTarget) != 32 {
		return out
	}
	ratio := float64(actualSec) / float64(desiredSec)
	if ratio < 1.0/clamp {
		ratio = 1.0 / clamp
	}
	if ratio > clamp {
		ratio = clamp
	}
	cur := new(big.Int).SetBytes(oldTarget)
	scale := big.NewFloat(ratio)
	f := new(big.Float).SetInt(cur)
	f.Mul(f, scale)
	adj, _ := f.Int(nil)
	if adj.BitLen() > 256 {
		adj.SetString("ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", 16)
	}
	b := adj.Bytes()
	copy(out[32-len(b):], b)
	return out
}

// ClampRatio for tests.
func ClampRatio(actual, desired int, clamp float64) float64 {
	if desired <= 0 || actual <= 0 {
		return 1
	}
	r := float64(actual) / float64(desired)
	if r < 1.0/clamp {
		return 1.0 / clamp
	}
	if r > clamp {
		return clamp
	}
	return r
}
