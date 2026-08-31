package fuzzengine

import (
	"sort"
	"strings"
)

const maxAutodictTokens = 48
const maxAutodictTokenLen = 32
const minAutodictTokenLen = 3
const maxAutodictDictBytes = 2048

// ExtractAutodictTokens scans corpus inputs for reusable splice tokens (JSON keys, XML tags, etc.).
func ExtractAutodictTokens(inputs ...[]byte) [][]byte {
	seen := map[string]struct{}{}
	out := make([][]byte, 0, maxAutodictTokens)
	add := func(tok []byte) {
		if len(tok) < minAutodictTokenLen || len(tok) > maxAutodictTokenLen {
			return
		}
		key := string(tok)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, append([]byte(nil), tok...))
	}
	for _, input := range inputs {
		if len(input) == 0 {
			continue
		}
		s := string(input)
		// Quoted strings.
		for i := 0; i < len(input); i++ {
			if input[i] != '"' && input[i] != '\'' {
				continue
			}
			q := input[i]
			j := i + 1
			for j < len(input) && input[j] != q {
				j++
			}
			if j > i+1 {
				add(input[i+1 : j])
			}
			i = j
		}
		// JSON-ish keys: "key":
		for _, part := range strings.Split(s, "\"") {
			part = strings.TrimSpace(part)
			if len(part) >= minAutodictTokenLen && len(part) <= maxAutodictTokenLen {
				if strings.HasSuffix(part, ":") {
					part = strings.TrimSuffix(part, ":")
				}
				if isAutodictToken(part) {
					add([]byte(part))
				}
			}
		}
		// XML-ish tags.
		for _, seg := range strings.Split(s, "<") {
			seg = strings.TrimSpace(seg)
			if seg == "" {
				continue
			}
			if idx := strings.IndexAny(seg, " />"); idx > 0 {
				seg = seg[:idx]
			}
			if isAutodictToken(seg) {
				add([]byte(seg))
			}
		}
		if len(out) >= maxAutodictTokens {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) == len(out[j]) {
			return string(out[i]) < string(out[j])
		}
		return len(out[i]) < len(out[j])
	})
	if len(out) > maxAutodictTokens {
		out = out[:maxAutodictTokens]
	}
	return out
}

func isAutodictToken(s string) bool {
	if len(s) < minAutodictTokenLen || len(s) > maxAutodictTokenLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			continue
		}
		return false
	}
	return true
}

// MergeAutodict merges static dictionary bytes with corpus-derived tokens.
func MergeAutodict(static []byte, tokens [][]byte) []byte {
	if len(tokens) == 0 {
		return append([]byte(nil), static...)
	}
	seen := map[string]struct{}{}
	out := make([]byte, 0, len(static)+256)
	appendTok := func(tok []byte) {
		if len(tok) == 0 {
			return
		}
		key := string(tok)
		if _, ok := seen[key]; ok {
			return
		}
		if len(out)+len(tok) > maxAutodictDictBytes {
			return
		}
		seen[key] = struct{}{}
		out = append(out, tok...)
	}
	appendTok(static)
	for _, tok := range tokens {
		appendTok(tok)
	}
	return out
}

// CorpusBytesFromSeeds extracts byte inputs from pool corpus seeds.
func CorpusBytesFromSeeds(seeds []PoolCorpusSeed) [][]byte {
	if len(seeds) == 0 {
		return nil
	}
	out := make([][]byte, 0, len(seeds))
	for _, s := range seeds {
		if len(s.InputBytes) > 0 {
			out = append(out, s.InputBytes)
		}
	}
	return out
}

// EffectiveMutatorDict merges config dict with optional corpus autodict tokens.
func EffectiveMutatorDict(cfg map[string]any, corpus [][]byte) []byte {
	base := ParseMutatorDict(cfg)
	if len(corpus) == 0 {
		return base
	}
	return MergeAutodict(base, ExtractAutodictTokens(corpus...))
}
