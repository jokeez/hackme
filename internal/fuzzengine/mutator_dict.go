package fuzzengine

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
)

// ParseMutatorDict reads optional pack-specific splice dictionary from config.
// JSON round-trips encode []byte as standard base64; accept hex, base64, or raw text.
func ParseMutatorDict(cfg map[string]any) []byte {
	if cfg == nil {
		return nil
	}
	raw, ok := cfg["mutator_dict"]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []byte:
		if len(v) == 0 {
			return nil
		}
		out := make([]byte, len(v))
		copy(out, v)
		return out
	case string:
		s := strings.TrimSpace(v)
		if s == "" {
			return nil
		}
		// Prefer hex (Dig Finalize stores hex for stable config_json).
		if b, err := hex.DecodeString(s); err == nil && len(b) > 0 && len(s)%2 == 0 {
			return b
		}
		// encoding/json marshals []byte as std base64 — recover those dicts.
		if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) > 0 {
			return b
		}
		return []byte(s)
	case []any:
		out := make([]byte, 0, len(v))
		for _, item := range v {
			switch x := item.(type) {
			case string:
				if b, err := hex.DecodeString(strings.TrimSpace(x)); err == nil {
					out = append(out, b...)
				} else if x != "" {
					out = append(out, x...)
				}
			case float64:
				out = append(out, byte(int(x)&0xff))
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	default:
		return nil
	}
}

func dictPickFrom(mix uint64, dict []byte) byte {
	if len(dict) == 0 {
		return dictPick(mix)
	}
	return dict[mix%uint64(len(dict))]
}
