package fuzzengine

import (
	"sort"
	"strings"
)

const maxAutodictTokens = 96
const maxAutodictTokenLen = 32
const minAutodictTokenLen = 2
const maxAutodictDictBytes = 3072

// ExtractAutodictTokens scans corpus inputs for reusable splice tokens (JSON keys, XML tags, etc.).
// v2.8: frequency-ranked, includes magic bytes, path segments, and CmpLog ASCII constants.
func ExtractAutodictTokens(inputs ...[]byte) [][]byte {
	freq := map[string]int{}
	add := func(tok []byte) {
		if len(tok) < minAutodictTokenLen || len(tok) > maxAutodictTokenLen {
			return
		}
		freq[string(tok)]++
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
		// Numeric / path-ish fields.
		for _, part := range strings.FieldsFunc(s, func(r rune) bool {
			return r == ',' || r == ':' || r == '{' || r == '}' || r == '[' || r == ']' ||
				r == ' ' || r == '\n' || r == '\r' || r == '\t' || r == '=' || r == '?' || r == '&' || r == '/'
		}) {
			part = strings.TrimSpace(part)
			if len(part) < minAutodictTokenLen || len(part) > maxAutodictTokenLen {
				continue
			}
			allNum := true
			for _, c := range part {
				if c < '0' || c > '9' {
					allNum = false
					break
				}
			}
			if allNum || isAutodictToken(part) {
				add([]byte(part))
			}
		}
		// Escape sequences (\n, \t, \uXXXX).
		for i := 0; i+1 < len(input); i++ {
			if input[i] != '\\' {
				continue
			}
			if i+5 < len(input) && input[i+1] == 'u' {
				add(input[i : i+6])
			} else if i+1 < len(input) {
				add(input[i : i+2])
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
		// File / protocol magic prefixes (4–8 bytes).
		if len(input) >= 4 {
			add(input[:4])
		}
		if len(input) >= 8 {
			add(input[:8])
		}
		// Binary n-grams at stride (rare non-ASCII density).
		stride := 1
		if len(input) > 64 {
			stride = 4
		}
		if len(input) > 256 {
			stride = 8
		}
		for i := 0; i+3 < len(input); i += stride {
			nonzero := 0
			for j := 0; j < 4; j++ {
				if input[i+j] != 0 {
					nonzero++
				}
			}
			if nonzero >= 2 {
				add(input[i : i+4])
			}
		}
	}
	// Merge CmpLog ASCII/hex constants harvested from the same corpus.
	for _, tok := range ExtractCmpConstants(inputs...) {
		if len(tok) >= minAutodictTokenLen && len(tok) <= maxAutodictTokenLen {
			freq[string(tok)]++
		}
	}

	type scored struct {
		tok   string
		count int
	}
	list := make([]scored, 0, len(freq))
	for tok, c := range freq {
		list = append(list, scored{tok: tok, count: c})
	}
	// Prefer frequent + mid-length tokens (AFL autodict bias).
	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		li, lj := len(list[i].tok), len(list[j].tok)
		if li != lj {
			// Prefer 3–12 byte tokens over tiny/huge.
			si := autodictLenScore(li)
			sj := autodictLenScore(lj)
			if si != sj {
				return si > sj
			}
			return li < lj
		}
		return list[i].tok < list[j].tok
	})
	out := make([][]byte, 0, maxAutodictTokens)
	for _, sc := range list {
		if len(out) >= maxAutodictTokens {
			break
		}
		out = append(out, []byte(sc.tok))
	}
	return out
}

func autodictLenScore(n int) int {
	switch {
	case n >= 3 && n <= 12:
		return 3
	case n == 2 || (n > 12 && n <= 20):
		return 2
	default:
		return 1
	}
}

func isAutodictToken(s string) bool {
	if len(s) < minAutodictTokenLen || len(s) > maxAutodictTokenLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' || c == '.' {
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
