package fuzzengine

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash/fnv"
	"strings"
)

// DefaultByteSeedCorpus returns minimal tx-like seeds for byte-mode campaigns.
func DefaultByteSeedCorpus() []any {
	return []any{
		"0100000001", // minimal tx prefix (hex)
		"0200000001",
		"ff" + strings.Repeat("00", 7),
		base64.StdEncoding.EncodeToString([]byte{0x01, 0x00, 0x00, 0x00, 0x01}),
	}
}

// ParseByteCorpus reads seed_byte_corpus ([]hex strings, base64, or []byte).
func ParseByteCorpus(cfg map[string]any) [][]byte {
	if cfg == nil {
		return nil
	}
	raw, ok := cfg["seed_byte_corpus"]
	if !ok || raw == nil {
		return nil
	}
	switch t := raw.(type) {
	case []any:
		out := make([][]byte, 0, len(t))
		for _, item := range t {
			if b, ok := parseByteSeedAny(item); ok && len(b) > 0 {
				out = append(out, b)
			}
		}
		return out
	case []string:
		out := make([][]byte, 0, len(t))
		for _, item := range t {
			if b, ok := parseByteSeedAny(item); ok && len(b) > 0 {
				out = append(out, b)
			}
		}
		return out
	default:
		return nil
	}
}

func parseByteSeedAny(v any) ([]byte, bool) {
	switch x := v.(type) {
	case []byte:
		return append([]byte(nil), x...), true
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return nil, false
		}
		if strings.HasPrefix(strings.ToLower(s), "0x") {
			b, err := hex.DecodeString(s[2:])
			return b, err == nil && len(b) > 0
		}
		if b, err := hex.DecodeString(s); err == nil && len(b) > 0 {
			return b, true
		}
		if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) > 0 {
			return b, true
		}
		return []byte(s), len(s) > 0
	default:
		return nil, false
	}
}

// DeriveInputBytes maps work id to mutated byte input (deterministic anchor exec).
func DeriveInputBytes(inputN uint64, cfg map[string]any) []byte {
	_, b := SegmentExecInput(inputN, 0, cfg, nil)
	return b
}

// InputBytesSHA256 hashes arbitrary fuzz input bytes.
func InputBytesSHA256(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// PackInputBytesToU64 packs up to 8 little-endian bytes for legacy check(i64).
func PackInputBytesToU64(b []byte) uint64 {
	var u uint64
	for i := 0; i < len(b) && i < 8; i++ {
		u |= uint64(b[i]) << (8 * i)
	}
	return u
}

// ReproCmdBytes returns a copy-paste repro line for byte inputs.
func ReproCmdBytes(wasmPath string, input []byte) string {
	hexIn := hex.EncodeToString(input)
	if strings.TrimSpace(wasmPath) == "" {
		return fmt.Sprintf("check_bytes(%q) /* len=%d */", hexIn, len(input))
	}
	return fmt.Sprintf("HACKME_FUZZ_INPUT_HEX=%s wasm=%s", hexIn, wasmPath)
}

// CoverageBucketsFromBytes returns input fingerprint buckets for corpus scheduling (not WASM edge coverage).
func CoverageBucketsFromBytes(input []byte) (edgeBucket, pathBucket int) {
	h := fnv.New64a()
	_, _ = h.Write(input)
	mix := h.Sum64()
	if len(input) == 0 {
		edgeBucket = int(mix % 257)
		pathBucket = int((mix ^ 1) % 509)
		return edgeBucket, pathBucket
	}
	edgeBucket = int((mix ^ uint64(len(input)*9973)) % 257)
	pathBucket = int((mix ^ uint64(int(input[0])*17+len(input))) % 509)
	if edgeBucket < 0 {
		edgeBucket = 0
	}
	if pathBucket < 0 {
		pathBucket = 0
	}
	return edgeBucket, pathBucket
}

// BytesDetectorTitle builds a short customer-facing title for byte-mode detector hits.
func BytesDetectorTitle(input []byte) string {
	const maxShow = 48
	printable := true
	for _, c := range input {
		if c == '\n' || c == '\t' || c == '\r' {
			continue
		}
		if c < 32 || c >= 127 {
			printable = false
			break
		}
	}
	if printable && len(input) > 0 {
		s := string(input)
		if len(s) > maxShow {
			s = s[:maxShow] + "…"
		}
		return "detector hit: " + s
	}
	hx := hex.EncodeToString(input)
	if len(hx) > maxShow*2 {
		hx = hx[:maxShow*2] + "…"
	}
	return "detector hit (bytes): 0x" + hx
}

