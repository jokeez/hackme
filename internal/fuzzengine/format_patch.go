package fuzzengine

// patchVarintLE flips a protobuf-style varint length prefix at off (or inserts one).
func patchVarintLE(buf []byte, off int, mix uint64, maxLen int) []byte {
	if len(buf) == 0 {
		return buf
	}
	out := append([]byte(nil), buf...)
	if off < 0 {
		off = 0
	}
	if off >= len(out) {
		off = len(out) - 1
	}
	v := uint64(mix & 0x7f)
	if mix&0x80 != 0 {
		v |= 0x80
	}
	if mix&0x100 != 0 && len(out)+1 <= maxLen {
		// insert fresh varint at start
		ins := []byte{byte(v & 0xff)}
		if v > 0x7f {
			ins = append(ins, byte((v>>7)&0xff)|0x80)
		}
		return append(ins, out...)
	}
	out[off] = byte(v & 0xff)
	return out
}

// insertInvalidUTF8 splices malformed UTF-8 sequences (parser footgun).
func insertInvalidUTF8(buf []byte, idx int, mix uint64, maxLen int) []byte {
	seqs := [][]byte{
		{0xc0, 0x80},             // overlong null
		{0xed, 0xa0, 0x80},       // surrogate
		{0xff, 0xfe},             // invalid lead
		{0xe0, 0x80, 0x80},       // overlong
		{0xf4, 0x90, 0x80, 0x80}, // > U+10FFFF
	}
	tok := seqs[int(mix%uint64(len(seqs)))]
	return insertToken(buf, idx, tok, maxLen)
}

// rotateBlock rotates a byte window (cheap structural change).
func rotateBlock(buf []byte, mix uint64) []byte {
	if len(buf) < 4 {
		return buf
	}
	out := append([]byte(nil), buf...)
	start := int(mix % uint64(len(out)-2))
	n := 2 + int((mix>>8)%uint64(len(out)-start-1))
	if start+n > len(out) {
		n = len(out) - start
	}
	if n < 2 {
		return out
	}
	chunk := append([]byte(nil), out[start:start+n]...)
	rot := int((mix >> 16) % uint64(n))
	if rot > 0 {
		copy(out[start:start+n], append(chunk[rot:], chunk[:rot]...))
	}
	return out
}

// parityFlip toggles high bit on a run (binary format stress).
func parityFlip(buf []byte, mix uint64) []byte {
	if len(buf) == 0 {
		return buf
	}
	out := append([]byte(nil), buf...)
	start := int(mix % uint64(len(out)))
	n := 1 + int((mix>>8)%4)
	for j := 0; j < n && start+j < len(out); j++ {
		out[start+j] ^= 0x80
	}
	return out
}
