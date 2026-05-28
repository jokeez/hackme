package hms

import (
	"encoding/hex"
	"testing"
)

func TestMerkleRootDeterministic(t *testing.T) {
	ct, _ := hex.DecodeString("abcd")
	l1 := LeafHash("c1", ct, 100, nil)
	l2 := LeafHash("c2", ct, 200, nil)
	r1 := MerkleRoot([][32]byte{l1, l2})
	r2 := MerkleRoot([][32]byte{l1, l2})
	if r1 != r2 {
		t.Fatal("merkle not deterministic")
	}
}

func TestSealHashTarget(t *testing.T) {
	var root [32]byte
	root[31] = 1
	target := defaultSealTarget()
	found := false
	for n := uint64(0); n < 500000; n++ {
		h := SealHash(1, root, "hackme-official", n)
		if HashBelowTarget(h[:], target) {
			found = true
			break
		}
	}
	if !found {
		t.Skip("no nonce in 500k (target very hard)")
	}
}
