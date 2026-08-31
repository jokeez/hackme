package fuzzengine

import (
	"encoding/binary"
	"sort"
)

// Interesting8 returns AFL-class single-byte values for parser fuzzing.
func Interesting8() []byte {
	return []byte{0, 1, 0x7f, 0x80, 0xff}
}

// Interesting16LE returns little-endian 16-bit interesting values.
func Interesting16LE() []uint16 {
	return []uint16{0, 1, 0x7f, 0x80, 0xff, 0x100, 0x7fff, 0xffff}
}

// Interesting32LE returns little-endian 32-bit interesting values.
func Interesting32LE() []uint32 {
	return []uint32{0, 1, 0x7f, 0x80, 0xff, 0x100, 0x7fff, 0xffff, 0x10000, 0x7fffffff, 0xffffffff}
}

func writeU16LE(buf []byte, off int, v uint16) {
	if off < 0 || off+1 >= len(buf) {
		return
	}
	binary.LittleEndian.PutUint16(buf[off:], v)
}

func writeU32LE(buf []byte, off int, v uint32) {
	if off < 0 || off+3 >= len(buf) {
		return
	}
	binary.LittleEndian.PutUint32(buf[off:], v)
}

func arithAdd8(buf []byte, off int, delta int8) {
	if off < 0 || off >= len(buf) {
		return
	}
	buf[off] = byte(int8(buf[off]) + delta)
}

func arithAdd16LE(buf []byte, off int, delta int16) {
	if off < 0 || off+1 >= len(buf) {
		return
	}
	v := int16(binary.LittleEndian.Uint16(buf[off:]))
	v += delta
	binary.LittleEndian.PutUint16(buf[off:], uint16(v))
}

func dictTokenAt(dict []byte, mix uint64) []byte {
	tokens := ParseDictTokens(dict)
	if len(tokens) == 0 {
		return nil
	}
	return tokens[int(mix%uint64(len(tokens)))]
}

func insertToken(out []byte, idx int, tok []byte, maxLen int) []byte {
	if len(tok) == 0 || len(out)+len(tok) > maxLen {
		return out
	}
	if idx < 0 {
		idx = 0
	}
	if idx > len(out) {
		idx = len(out)
	}
	res := make([]byte, 0, len(out)+len(tok))
	res = append(res, out[:idx]...)
	res = append(res, tok...)
	res = append(res, out[idx:]...)
	return res
}

func overwriteWithToken(out []byte, idx int, tok []byte) []byte {
	if len(tok) == 0 || len(out) == 0 {
		return out
	}
	if idx < 0 {
		idx = 0
	}
	if idx >= len(out) {
		idx = len(out) - 1
	}
	n := len(tok)
	if idx+n > len(out) {
		n = len(out) - idx
	}
	copy(out[idx:idx+n], tok[:n])
	return out
}

// ParseDictTokens splits a flat dictionary byte stream into splice tokens.
func ParseDictTokens(dict []byte) [][]byte {
	if len(dict) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([][]byte, 0, 16)
	// Greedy scan: alphanumeric runs and quoted strings.
	for i := 0; i < len(dict); {
		if dict[i] == '"' || dict[i] == '\'' {
			q := dict[i]
			j := i + 1
			for j < len(dict) && dict[j] != q {
				j++
			}
			if j > i+1 {
				tok := string(dict[i+1 : j])
				if _, ok := seen[tok]; !ok && len(tok) >= 2 && len(tok) <= 32 {
					seen[tok] = struct{}{}
					out = append(out, []byte(tok))
				}
			}
			i = j + 1
			continue
		}
		if isDictTokenChar(dict[i]) {
			j := i + 1
			for j < len(dict) && isDictTokenChar(dict[j]) {
				j++
			}
			if j-i >= 2 && j-i <= 32 {
				tok := string(dict[i:j])
				if _, ok := seen[tok]; !ok {
					seen[tok] = struct{}{}
					out = append(out, append([]byte(nil), dict[i:j]...))
				}
			}
			i = j
			continue
		}
		// Single-byte punctuation tokens from static dicts.
		if dict[i] == '{' || dict[i] == '}' || dict[i] == '[' || dict[i] == ']' {
			tok := string(dict[i : i+1])
			if _, ok := seen[tok]; !ok {
				seen[tok] = struct{}{}
				out = append(out, []byte(tok))
			}
		}
		i++
	}
	sort.Slice(out, func(i, j int) bool { return len(out[i]) < len(out[j]) })
	return out
}

func isDictTokenChar(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_' || b == '-'
}
