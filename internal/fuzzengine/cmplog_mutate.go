package fuzzengine

import (
	"encoding/binary"
	"sort"
)

const cmpConstCap = 48

// ExtractCmpConstants harvests comparison-relevant tokens from corpus inputs (no instrumentation).
// Includes 2/4/8-byte windows (LE raw + BE byte-swapped), and ASCII decimal/hex runs length 2–16.
func ExtractCmpConstants(inputs ...[]byte) [][]byte {
	seen := map[string]struct{}{}
	raw := make([][]byte, 0, cmpConstCap)

	add := func(b []byte) {
		if len(b) < 2 || len(b) > 16 {
			return
		}
		k := string(b)
		if _, ok := seen[k]; ok {
			return
		}
		seen[k] = struct{}{}
		raw = append(raw, append([]byte(nil), b...))
	}

	addBE := func(b []byte) {
		if len(b) < 2 {
			return
		}
		rev := append([]byte(nil), b...)
		for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
			rev[i], rev[j] = rev[j], rev[i]
		}
		add(rev)
	}

	for _, inp := range inputs {
		if len(inp) == 0 {
			continue
		}
		// Prefer ASCII/hex runs (high signal for CmpLog-style solves).
		scanASCIIRuns(inp, add)
		// Stride-sample binary windows — full slide is O(n) noise on large seeds.
		stride := 1
		if len(inp) > 32 {
			stride = 2
		}
		if len(inp) > 128 {
			stride = 4
		}
		if len(inp) > 512 {
			stride = 8
		}
		for i := 0; i+2 <= len(inp); i += stride {
			if i+2 <= len(inp) {
				w := inp[i : i+2]
				if w[0] != 0 || w[1] != 0 {
					add(w)
					addBE(w)
				}
			}
			if i+4 <= len(inp) {
				w := inp[i : i+4]
				nonzero := 0
				for _, b := range w {
					if b != 0 {
						nonzero++
					}
				}
				if nonzero >= 1 {
					add(w)
					addBE(w)
				}
			}
			if i+8 <= len(inp) && (i%(stride*2) == 0) {
				w := inp[i : i+8]
				add(w)
				addBE(w)
			}
			if len(seen) > cmpConstCap*4 {
				break
			}
		}
	}

	sort.Slice(raw, func(i, j int) bool {
		if len(raw[i]) != len(raw[j]) {
			return len(raw[i]) < len(raw[j])
		}
		return string(raw[i]) < string(raw[j])
	})
	return capCmpConstants(raw, cmpConstCap)
}

func capCmpConstants(raw [][]byte, capN int) [][]byte {
	if len(raw) <= capN {
		return raw
	}
	// raw is sorted ascending; prefer longest tokens when trimming.
	out := make([][]byte, 0, capN)
	for i := len(raw) - 1; i >= 0 && len(out) < capN; i-- {
		out = append(out, raw[i])
	}
	sort.Slice(out, func(i, j int) bool {
		if len(out[i]) != len(out[j]) {
			return len(out[i]) < len(out[j])
		}
		return string(out[i]) < string(out[j])
	})
	return out
}

func scanASCIIRuns(inp []byte, add func([]byte)) {
	for i := 0; i < len(inp); {
		if isDecimal(inp[i]) {
			j := i + 1
			for j < len(inp) && isDecimal(inp[j]) {
				j++
			}
			if n := j - i; n >= 2 && n <= 16 {
				add(inp[i:j])
			}
			i = j
			continue
		}
		if i+2 <= len(inp) && inp[i] == '0' && (inp[i+1] == 'x' || inp[i+1] == 'X') {
			j := i + 2
			for j < len(inp) && isHex(inp[j]) {
				j++
			}
			if n := j - i; n >= 2 && n <= 16 {
				add(inp[i:j])
			}
			i = j
			continue
		}
		if isHex(inp[i]) {
			j := i + 1
			for j < len(inp) && isHex(inp[j]) {
				j++
			}
			if n := j - i; n >= 2 && n <= 16 {
				add(inp[i:j])
			}
			i = j
			continue
		}
		i++
	}
}

func isDecimal(b byte) bool { return b >= '0' && b <= '9' }

func isHex(b byte) bool {
	return isDecimal(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// cmpReplaceWithInteresting overwrites a 1/2/4-byte window with an AFL interesting value.
func cmpReplaceWithInteresting(buf []byte, mix uint64) []byte {
	if len(buf) == 0 {
		return buf
	}
	out := append([]byte(nil), buf...)
	be := mix&1 == 1
	width := 1 + int((mix>>4)%3)
	switch width {
	case 1:
		off := int(mix % uint64(len(out)))
		vals := Interesting8()
		out[off] = vals[int((mix>>8)%uint64(len(vals)))]
	case 2:
		if len(out) < 2 {
			return out
		}
		off := int(mix % uint64(len(out)-1))
		var vals []uint16
		if be {
			vals = Interesting16BE()
		} else {
			vals = Interesting16LE()
		}
		v := vals[int((mix>>8)%uint64(len(vals)))]
		if be {
			writeU16BE(out, off, v)
		} else {
			writeU16LE(out, off, v)
		}
	default:
		if len(out) < 4 {
			return out
		}
		off := int(mix % uint64(len(out)-3))
		var vals []uint32
		if be {
			vals = Interesting32BE()
		} else {
			vals = Interesting32LE()
		}
		v := vals[int((mix>>8)%uint64(len(vals)))]
		if be {
			writeU32BE(out, off, v)
		} else {
			writeU32LE(out, off, v)
		}
	}
	return out
}

// cmpArithTowardInteresting adds a delta at a LE integer field to move toward interesting magnitudes.
func cmpArithTowardInteresting(buf []byte, mix uint64) []byte {
	if len(buf) == 0 {
		return buf
	}
	out := append([]byte(nil), buf...)
	mode := mix % 7
	if mode >= 4 {
		off := int(mix % uint64(len(out)))
		switch mode {
		case 4:
			arithAdd8(out, off, 1)
		case 5:
			arithAdd8(out, off, -1)
		case 6:
			if mix&0x100 != 0 && off+3 < len(out) {
				d := int32(256)
				if mix&0x200 != 0 {
					d = -256
				}
				arithAdd32LE(out, off, d)
			} else if off+1 < len(out) {
				d := int16(16)
				if mix&0x200 != 0 {
					d = -16
				}
				arithAdd16LE(out, off, d)
			} else {
				d := int8(16)
				if mix&0x200 != 0 {
					d = -16
				}
				arithAdd8(out, off, d)
			}
		}
		return out
	}

	width := 1 + int(mode) // 1, 2, or 4 bytes
	switch width {
	case 1:
		off := int(mix % uint64(len(out)))
		cur := out[off]
		target := nearestInteresting8(cur, mix)
		arithAdd8(out, off, int8(target)-int8(cur))
	case 2:
		if len(out) < 2 {
			return out
		}
		off := int(mix % uint64(len(out)-1))
		cur := binary.LittleEndian.Uint16(out[off:])
		target := nearestInteresting16(cur, mix)
		arithAdd16LE(out, off, int16(target)-int16(cur))
	default:
		if len(out) < 4 {
			return out
		}
		off := int(mix % uint64(len(out)-3))
		cur := binary.LittleEndian.Uint32(out[off:])
		target := nearestInteresting32(cur, mix)
		arithAdd32LE(out, off, int32(target)-int32(cur))
	}
	return out
}

func nearestInteresting8(cur byte, mix uint64) byte {
	vals := Interesting8()
	best := vals[0]
	var bestDist uint
	for i, v := range vals {
		d := absU8(cur, v)
		if i == 0 || d < bestDist || (d == bestDist && v < best) {
			best, bestDist = v, d
		}
	}
	_ = mix
	return best
}

func nearestInteresting16(cur uint16, mix uint64) uint16 {
	vals := Interesting16LE()
	if mix&1 == 1 {
		vals = Interesting16BE()
	}
	best := vals[0]
	var bestDist uint32
	for i, v := range vals {
		var d uint32
		if cur > v {
			d = uint32(cur - v)
		} else {
			d = uint32(v - cur)
		}
		if i == 0 || d < bestDist {
			best, bestDist = v, d
		}
	}
	return best
}

func nearestInteresting32(cur uint32, mix uint64) uint32 {
	vals := Interesting32LE()
	if mix&1 == 1 {
		vals = Interesting32BE()
	}
	best := vals[0]
	var bestDist uint64
	for i, v := range vals {
		var d uint64
		if cur > v {
			d = uint64(cur - v)
		} else {
			d = uint64(v - cur)
		}
		if i == 0 || d < bestDist {
			best, bestDist = v, d
		}
	}
	return best
}

func absU8(a, b byte) uint {
	if a > b {
		return uint(a - b)
	}
	return uint(b - a)
}

// cmpSwapWithCorpusConst splices or overwrites with a corpus-derived comparison constant.
func cmpSwapWithCorpusConst(buf []byte, corpusConsts [][]byte, mix uint64) []byte {
	if len(buf) == 0 {
		return buf
	}
	if len(corpusConsts) == 0 {
		return append([]byte(nil), buf...)
	}
	tok := corpusConsts[int(mix%uint64(len(corpusConsts)))]
	if len(tok) == 0 {
		return append([]byte(nil), buf...)
	}
	out := append([]byte(nil), buf...)
	idx := int((mix >> 8) % uint64(len(out)+1))
	if mix&4 != 0 && idx+len(tok) <= len(out) {
		return overwriteWithToken(out, idx, tok)
	}
	// insert (no maxLen here — caller caps elsewhere)
	if len(out)+len(tok) > DefaultMaxInputBytesStd {
		return overwriteWithToken(out, min(idx, len(out)-1), tok)
	}
	return insertToken(out, idx, tok, DefaultMaxInputBytesStd)
}

// cmpXorWindow XORs a 2- or 4-byte window with an interesting magnitude.
func cmpXorWindow(buf []byte, mix uint64) []byte {
	if len(buf) < 2 {
		return buf
	}
	out := append([]byte(nil), buf...)
	wide := mix&1 == 1
	if wide && len(out) >= 4 {
		off := int(mix % uint64(len(out)-3))
		vals := Interesting32LE()
		v := vals[int((mix>>8)%uint64(len(vals)))]
		cur := binary.LittleEndian.Uint32(out[off:])
		binary.LittleEndian.PutUint32(out[off:], cur^v)
		return out
	}
	off := int(mix % uint64(len(out)-1))
	vals := Interesting16LE()
	v := vals[int((mix>>8)%uint64(len(vals)))]
	cur := binary.LittleEndian.Uint16(out[off:])
	binary.LittleEndian.PutUint16(out[off:], cur^v)
	return out
}

// cmpInsertBoundary inserts length/boundary comparison footguns at a deterministic index.
func cmpInsertBoundary(buf []byte, mix uint64, maxLen int) []byte {
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	tokens := [][]byte{
		{0xff, 0xff},
		{0xff, 0xff, 0xff, 0xff},
		{0xff, 0x7f, 0xff, 0xff},
		{0x7f, 0xff, 0xff, 0xff},
		[]byte("-1"),
		[]byte("0x7fffffff"),
		{0xff, 0xff, 0xff, 0x7f},
		{0x7f, 0xff, 0xff, 0xff},
	}
	tok := tokens[int(mix%uint64(len(tokens)))]
	idx := int((mix >> 8) % uint64(len(buf)+1))
	return insertToken(buf, idx, tok, maxLen)
}
