package fuzzengine

// FounderB v2.8/v2.9 helpers retained alongside upstream DeepHavocV28.

func spliceCmpConst(buf []byte, corpus [][]byte, mix uint64, maxLen int) []byte {
	consts := ExtractCmpConstants(corpus...)
	if len(consts) == 0 {
		return append([]byte(nil), buf...)
	}
	tok := consts[int(mix%uint64(len(consts)))]
	idx := int((mix >> 8) % uint64(len(buf)+1))
	return insertToken(buf, idx, tok, maxLen)
}

func repeatRareNibble(buf []byte, mix uint64) []byte {
	if len(buf) == 0 {
		return buf
	}
	out := append([]byte(nil), buf...)
	// nibbles that rarely appear in ASCII protocols
	rare := []byte{0x0d, 0x0e, 0x0f, 0x1b, 0x1c, 0x1d, 0x1e, 0x1f}
	b := rare[int(mix%uint64(len(rare)))]
	n := 2 + int((mix>>8)%6)
	start := int(mix % uint64(len(out)))
	for i := 0; i < n && start+i < len(out); i++ {
		out[start+i] = (out[start+i] & 0xf0) | (b & 0x0f)
	}
	return out
}

// insertUTF8Overlong splices overlong UTF-8 encodings (distinct from insertInvalidUTF8 sequences).

func insertUTF8Overlong(buf []byte, idx int, mix uint64, maxLen int) []byte {
	seqs := [][]byte{
		{0xc1, 0xbf},                   // overlong '/'
		{0xe0, 0x81, 0xbf},             // overlong 2-byte
		{0xf0, 0x80, 0x80, 0x80},       // overlong 3-byte
		{0xf8, 0x80, 0x80, 0x80, 0x80}, // 5-byte (invalid lead)
		{0xfc, 0x80, 0x80, 0x80, 0x80, 0x80},
	}
	tok := seqs[int(mix%uint64(len(seqs)))]
	return insertToken(buf, idx, tok, maxLen)
}

func widenThenNarrow(buf []byte, mix uint64, maxLen int) []byte {
	if len(buf) == 0 || maxLen <= 0 {
		return buf
	}
	dup := append(append([]byte(nil), buf...), buf...)
	if len(dup) > maxLen {
		dup = dup[:maxLen]
	}
	half := len(dup) / 2
	if half < 1 {
		return dup
	}
	// deterministic skew: drop from front or back
	if mix&1 == 0 {
		return dup[:half]
	}
	return dup[len(dup)-half:]
}

func interleaveCorpus(buf []byte, corpus [][]byte, mix uint64, maxLen int) []byte {
	if len(corpus) == 0 || len(buf) == 0 {
		return append([]byte(nil), buf...)
	}
	other := corpus[int(mix%uint64(len(corpus)))]
	if len(other) == 0 {
		return append([]byte(nil), buf...)
	}
	n := min(len(buf), len(other), 64)
	out := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		if mix&2 == 0 {
			out = append(out, buf[i], other[i])
		} else {
			out = append(out, other[i], buf[i])
		}
	}
	// append remainder of longer side
	if len(buf) > n {
		out = append(out, buf[n:]...)
	} else if len(other) > n {
		out = append(out, other[n:]...)
	}
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}
