package block

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
)

// ZeroPrevHash is the previous hash for the genesis block (64 hex zeros).
const ZeroPrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

// headerForHash returns canonical bytes for hashing (excludes Hash field).
func (b *Block) headerForHash() []byte {
	type wire struct {
		Index        uint64 `json:"index"`
		Timestamp    int64  `json:"timestamp_unix"`
		PrevHash     string `json:"prev_hash"`
		Nonce        uint64 `json:"nonce"`
		MinerAddress string `json:"miner_address"`
		Task         Task   `json:"task"`
	}
	w := wire{
		Index:        b.Index,
		Timestamp:    b.Timestamp,
		PrevHash:     b.PrevHash,
		Nonce:        b.Nonce,
		MinerAddress: b.MinerAddress,
		Task:         b.Task,
	}
	buf, err := json.Marshal(w)
	if err != nil {
		panic(err)
	}
	return buf
}

// SetHash computes SHA-256 over canonical header JSON and assigns hex to Hash.
func (b *Block) SetHash() {
	b.Hash = b.HeaderHashHex()
}

// HeaderHashHex computes SHA-256 over canonical header JSON and returns hex.
func (b *Block) HeaderHashHex() string {
	sum := sha256.Sum256(b.headerForHash())
	return hex.EncodeToString(sum[:])
}

// EvalLock WASM: eval(n) = n*7+13 (see sandbox package). Host-only helper for display.
func EvalLockHost(n uint64) uint64 {
	return n*7 + 13
}

// NonceLE8 returns 8-byte little-endian nonce for WASM memory / host checks.
func NonceLE8(n uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], n)
	return buf[:]
}
