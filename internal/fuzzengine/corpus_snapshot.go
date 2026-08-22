package fuzzengine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

type corpusSeedWire struct {
	InputU64      uint64 `json:"input_u64"`
	InputBytesHex string `json:"input_bytes_hex,omitempty"`
	Energy        int    `json:"energy"`
	Edge          int    `json:"edge"`
	Path          int    `json:"path"`
}

// EncodeCorpusSnapshot serializes pool corpus seeds for claim-time freeze (Phase 2).
func EncodeCorpusSnapshot(seeds []PoolCorpusSeed) (jsonBytes []byte, sha256Hex string, err error) {
	wire := make([]corpusSeedWire, len(seeds))
	for i, s := range seeds {
		wire[i] = corpusSeedWire{
			InputU64:      s.Input,
			InputBytesHex: hex.EncodeToString(s.InputBytes),
			Energy:        s.Energy,
			Edge:          s.Edge,
			Path:          s.Path,
		}
	}
	jsonBytes, err = json.Marshal(wire)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(jsonBytes)
	return jsonBytes, hex.EncodeToString(sum[:]), nil
}

// DecodeCorpusSnapshot restores seeds from a frozen claim snapshot.
func DecodeCorpusSnapshot(jsonBytes []byte) ([]PoolCorpusSeed, error) {
	if len(jsonBytes) == 0 {
		return nil, nil
	}
	var wire []corpusSeedWire
	if err := json.Unmarshal(jsonBytes, &wire); err != nil {
		return nil, err
	}
	out := make([]PoolCorpusSeed, 0, len(wire))
	for _, w := range wire {
		var b []byte
		if h := stringsTrim(w.InputBytesHex); h != "" {
			dec, err := hex.DecodeString(h)
			if err != nil {
				return nil, fmt.Errorf("corpus snapshot: bad input_bytes_hex: %w", err)
			}
			b = dec
		}
		out = append(out, PoolCorpusSeed{
			Input: w.InputU64, InputBytes: b, Energy: w.Energy, Edge: w.Edge, Path: w.Path,
		})
	}
	return out, nil
}

// CorpusSeedsClaimMaps builds coordinator claim JSON for frozen corpus seeds.
func CorpusSeedsClaimMaps(seeds []PoolCorpusSeed) []map[string]any {
	if len(seeds) == 0 {
		return nil
	}
	out := make([]map[string]any, len(seeds))
	for i, s := range seeds {
		m := map[string]any{
			"input_u64": s.Input,
			"energy":    s.Energy,
			"edge":      s.Edge,
			"path":      s.Path,
		}
		if len(s.InputBytes) > 0 {
			m["input_bytes_hex"] = hex.EncodeToString(s.InputBytes)
		}
		out[i] = m
	}
	return out
}

// CorpusSeedsFromClaimMaps parses claim JSON corpus_seeds into PoolCorpusSeed slice.
func CorpusSeedsFromClaimMaps(raw []map[string]any) ([]PoolCorpusSeed, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]PoolCorpusSeed, 0, len(raw))
	for _, m := range raw {
		u := u64FromAny(m["input_u64"])
		energy := intFromAny(m["energy"])
		edge := intFromAny(m["edge"])
		path := intFromAny(m["path"])
		var b []byte
		if hx, ok := m["input_bytes_hex"].(string); ok && stringsTrim(hx) != "" {
			dec, err := hex.DecodeString(stringsTrim(hx))
			if err != nil {
				return nil, err
			}
			b = dec
		}
		out = append(out, PoolCorpusSeed{
			Input: u, InputBytes: b, Energy: energy, Edge: edge, Path: path,
		})
	}
	return out, nil
}

func stringsTrim(s string) string {
	i := 0
	j := len(s)
	for i < j && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\t' || s[j-1] == '\n' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}

func u64FromAny(v any) uint64 {
	switch x := v.(type) {
	case uint64:
		return x
	case int:
		if x < 0 {
			return 0
		}
		return uint64(x)
	case int64:
		if x < 0 {
			return 0
		}
		return uint64(x)
	case float64:
		if x < 0 {
			return 0
		}
		return uint64(x)
	default:
		return 0
	}
}
