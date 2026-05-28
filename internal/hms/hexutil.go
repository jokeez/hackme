package hms

import (
	"encoding/hex"
	"strings"
)

func encodeHex(b []byte) string {
	return hex.EncodeToString(b)
}

func decodeHex(s string) ([]byte, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(s), "0x"))
	return hex.DecodeString(s)
}

func decodeHex32(s string, fallback []byte) []byte {
	if strings.TrimSpace(s) == "" {
		out := make([]byte, 32)
		copy(out, fallback)
		return out
	}
	b, err := decodeHex(s)
	if err != nil || len(b) == 0 {
		out := make([]byte, 32)
		copy(out, fallback)
		return out
	}
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}
