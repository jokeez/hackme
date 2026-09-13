package fuzzengine

import (
	"encoding/binary"
	"sort"
)

// Interesting8 returns AFL-class single-byte values for parser fuzzing.
func Interesting8() []byte {
	return []byte{
		0, 1, 2, 3, 4, 7, 8, 15, 16, 31, 32, 63, 64,
		0x7f, 0x80, 0x81, 0xfe, 0xff,
	}
}

// Interesting16LE returns little-endian 16-bit interesting values.
func Interesting16LE() []uint16 {
	return []uint16{
		0, 1, 0x7f, 0x80, 0xff, 0x100, 0x200, 0x3ff, 0x400,
		0x7ff, 0x800, 0xfff, 0x1000, 0x7fff, 0x8000, 0xffff,
	}
}

// Interesting16BE returns big-endian interesting 16-bit values (same magnitudes).
func Interesting16BE() []uint16 {
	return Interesting16LE()
}

// Interesting32LE returns little-endian 32-bit interesting values.
func Interesting32LE() []uint32 {
	return []uint32{
		0, 1, 0x7f, 0x80, 0xff, 0x100, 0x7fff, 0x8000, 0xffff,
		0x10000, 0x20000, 0x3ffff, 0x7fffffff, 0x80000000, 0xffffffff,
	}
}

// Interesting32BE returns big-endian interesting 32-bit values.
func Interesting32BE() []uint32 {
	return Interesting32LE()
}

func writeU16LE(buf []byte, off int, v uint16) {
	if off < 0 || off+1 >= len(buf) {
		return
	}
	binary.LittleEndian.PutUint16(buf[off:], v)
}

func writeU16BE(buf []byte, off int, v uint16) {
	if off < 0 || off+1 >= len(buf) {
		return
	}
	binary.BigEndian.PutUint16(buf[off:], v)
}

func writeU32LE(buf []byte, off int, v uint32) {
	if off < 0 || off+3 >= len(buf) {
		return
	}
	binary.LittleEndian.PutUint32(buf[off:], v)
}

func writeU32BE(buf []byte, off int, v uint32) {
	if off < 0 || off+3 >= len(buf) {
		return
	}
	binary.BigEndian.PutUint32(buf[off:], v)
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

func arithAdd32LE(buf []byte, off int, delta int32) {
	if off < 0 || off+3 >= len(buf) {
		return
	}
	v := int32(binary.LittleEndian.Uint32(buf[off:]))
	v += delta
	binary.LittleEndian.PutUint32(buf[off:], uint32(v))
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

// structureSmash applies cheap format-aware damage (JSON/XML-ish) without a full grammar.
func structureSmash(out []byte, mix uint64, maxLen int) []byte {
	if len(out) == 0 {
		return out
	}
	switch mix % 6 {
	case 0: // duplicate a brace / bracket if present
		for _, c := range []byte{'{', '[', '<', '"'} {
			for i, b := range out {
				if b == c && len(out) < maxLen {
					res := make([]byte, 0, len(out)+1)
					res = append(res, out[:i+1]...)
					res = append(res, c)
					res = append(res, out[i+1:]...)
					return res
				}
			}
		}
	case 1: // drop a closing brace
		for i := len(out) - 1; i >= 0; i-- {
			if out[i] == '}' || out[i] == ']' || out[i] == '>' {
				return append(append([]byte(nil), out[:i]...), out[i+1:]...)
			}
		}
	case 2: // inject null mid-buffer
		idx := int(mix % uint64(len(out)))
		out = append([]byte(nil), out...)
		out[idx] = 0
		return out
	case 3: // UTF-8 overlong / BOM-ish prefix
		prefix := []byte{0xef, 0xbb, 0xbf, 0xc0, 0x80}
		if len(out)+len(prefix) <= maxLen {
			return append(prefix, out...)
		}
	case 4: // flip quoted region to unquoted junk
		out = append([]byte(nil), out...)
		for i := range out {
			if out[i] == '"' {
				out[i] = '\''
				break
			}
		}
		return out
	default: // repeat last printable run
		if len(out) < maxLen/2 {
			n := 1 + int(mix%8)
			chunk := out
			if len(chunk) > 16 {
				chunk = chunk[len(chunk)-16:]
			}
			for i := 0; i < n && len(out)+len(chunk) <= maxLen; i++ {
				out = append(out, chunk...)
			}
		}
	}
	return out
}

// crossoverBytes splices two corpus parents (AFL-style) for fleet diversity.
func crossoverBytes(a, b []byte, mix uint64, maxLen int) []byte {
	if len(a) == 0 {
		return append([]byte(nil), b...)
	}
	if len(b) == 0 {
		return append([]byte(nil), a...)
	}
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	cutA := int(mix % uint64(len(a)))
	cutB := int((mix >> 16) % uint64(len(b)))
	out := make([]byte, 0, len(a)+len(b))
	out = append(out, a[:cutA]...)
	out = append(out, b[cutB:]...)
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	if len(out) == 0 {
		return append([]byte(nil), a...)
	}
	return out
}
