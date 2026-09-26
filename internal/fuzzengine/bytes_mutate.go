package fuzzengine

// U64LayoutToBytes expands packed check(i64) layout to 8 little-endian bytes.
func U64LayoutToBytes(n uint64) []byte {
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[i] = byte(n >> (8 * i))
	}
	return buf[:]
}

// MutateBytes applies staged byte mutations: bitflip, insert, splice, opcode dict.
func MutateBytes(base []byte, stage MutationStage, salt uint64, maxLen int) []byte {
	return MutateBytesForConfig(base, stage, salt, maxLen, nil)
}
func MutateBytesWithDict(base []byte, stage MutationStage, salt uint64, maxLen int, dict []byte) []byte {
	return mutateBytesWithDict(base, stage, salt, maxLen, dict, nil)
}

// MutateBytesForHunt applies mutations with static dict + optional corpus autodict + crossover.
// When cfg enables havoc_deep_v28, a second deterministic deep stack is applied (replay-safe opt-in).
// v2.10 burst is a further opt-in (havoc_deep_v210) so legacy deep-v28 campaigns stay byte-stable.
func MutateBytesForHunt(base []byte, stage MutationStage, salt uint64, maxLen int, cfg map[string]any, corpus [][]byte) []byte {
	dict := EffectiveMutatorDict(cfg, corpus)
	out := mutateBytesWithDict(base, stage, salt, maxLen, dict, corpus)
	if DeepHavocV28(cfg) {
		out = applyDeepHavocV28(out, stage, salt, maxLen, dict, corpus)
	}
	if DeepHavocV210(cfg) {
		out = applyDeepV210Burst(out, stage, salt, maxLen, dict, corpus)
	}
	return out
}

// MutateBytesForConfig applies byte mutations with optional pack mutator_dict.
func MutateBytesForConfig(base []byte, stage MutationStage, salt uint64, maxLen int, cfg map[string]any) []byte {
	return mutateBytesWithDict(base, stage, salt, maxLen, ParseMutatorDict(cfg), nil)
}

// HavocOpModulo is the havoc op grid size (v2.8+: 80 CmpLog-aware ops).
// v2.9 selects ops via soft weight table (havocOpPick), not uniform modulo.
const HavocOpModulo = 80

// havocStackDepth returns how many stacked havoc ops to apply (AFL-like energy).
// Deterministic from stage+salt so coordinator replay stays stable.
func havocStackDepth(stage MutationStage, salt uint64) int {
	s := int(stage)
	rounds := 1 + int((salt+uint64(s))%7)
	if s >= StageHavocBase {
		extra := (s - StageHavocBase) / 2
		if extra > 12 {
			extra = 12
		}
		rounds += extra
		if (salt^uint64(s))%11 == 0 {
			rounds += 4
		}
		if (salt^uint64(s*17))%23 == 0 {
			rounds += 3
		}
		if (salt^uint64(s*31))%29 == 0 {
			rounds += 2
		}
		if (salt^uint64(s*41))%37 == 0 {
			rounds += 3 // v2.8 rare deep stacks
		}
	}
	if rounds < 1 {
		rounds = 1
	}
	if rounds > 36 {
		rounds = 36
	}
	return rounds
}

func applyDeterministicByteStage(out []byte, stage int, salt uint64) {
	if len(out) == 0 {
		return
	}
	s := stage % StageDeterministicMax
	mix := splitmix64(salt ^ uint64(s)*0x9e3779b97f4a7c15)
	switch {
	case s < 16:
		// Walking bitflip: stage picks bit lane; salt walks byte offset.
		bit := uint(s % 8)
		idx := int((mix ^ uint64(s)) % uint64(len(out)))
		out[idx] ^= byte(1 << bit)
		if len(out) > 1 && s >= 8 {
			idx2 := (idx + 1 + int(mix%uint64(len(out)-1))) % len(out)
			out[idx2] ^= byte(1 << ((bit + 1) % 8))
		}
	case s < 32:
		idx := int(mix % uint64(len(out)))
		delta := int8(1 + int(s-16)%35)
		if (mix>>8)&1 == 1 {
			delta = -delta
		}
		arithAdd8(out, idx, delta)
	case s < 48:
		vals := Interesting8()
		idx := int(mix % uint64(len(out)))
		out[idx] = vals[int(mix>>8)%len(vals)]
		if len(out) > 1 && s >= 40 {
			idx2 := (idx + 1) % len(out)
			out[idx2] = vals[int(mix>>16)%len(vals)]
		}
	default:
		idx := int(mix % uint64(len(out)))
		switch s % 4 {
		case 0:
			if len(out) >= 2 {
				vals := Interesting16LE()
				writeU16LE(out, idx%(len(out)-1), vals[int(mix>>8)%len(vals)])
			} else {
				out[idx] ^= 0xff
			}
		case 1:
			if len(out) >= 2 {
				vals := Interesting16BE()
				writeU16BE(out, idx%(len(out)-1), vals[int(mix>>8)%len(vals)])
			} else {
				out[idx] = 0
			}
		case 2:
			if len(out) >= 4 {
				vals := Interesting32LE()
				writeU32LE(out, idx%(len(out)-3), vals[int(mix>>8)%len(vals)])
			} else if len(out) >= 2 {
				arithAdd16LE(out, idx%(len(out)-1), int16(1+int(mix%35)))
			} else {
				arithAdd8(out, idx, 1)
			}
		default:
			if len(out) >= 4 {
				vals := Interesting32BE()
				writeU32BE(out, idx%(len(out)-3), vals[int(mix>>8)%len(vals)])
			} else if len(out) >= 2 {
				arithAdd16LE(out, idx%(len(out)-1), -int16(1+int(mix%35)))
			} else {
				arithAdd8(out, idx, -1)
			}
		}
	}
}
func mutateBytesWithDict(base []byte, stage MutationStage, salt uint64, maxLen int, dict []byte, corpus [][]byte) []byte {
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	if maxLen > MaxInputBytesHardCeil {
		maxLen = MaxInputBytesHardCeil
	}
	growCap := maxLen / 2
	if growCap < 64 {
		growCap = 64
	}
	if growCap > 512 {
		growCap = 512
	}
	if len(base) == 0 {
		return []byte{byte(salt & 0xff)}
	}
	s := int(stage)
	if s < StageDeterministicMax {
		out := append([]byte(nil), base...)
		applyDeterministicByteStage(out, s, salt)
		// v2.9: every 7th deterministic stage also applies CmpLog-inspired smash (replay-stable).
		if (salt % 7) == 0 {
			out = cmpReplaceWithInteresting(out, salt^uint64(s)*0x9E37)
			out = cmpArithTowardInteresting(out, salt^uint64(s)*0xC2B2)
		}
		if len(out) > maxLen {
			out = out[:maxLen]
		}
		return out
	}
	out := append([]byte(nil), base...)
	// Corpus crossover before havoc — fleet diversity (deterministic from salt).
	// v2.9: denser pre-havoc (every 3rd) + ordered/two-point splice path.
	if len(corpus) >= 2 && (salt%3) == 0 {
		other := corpus[int((salt>>8)%uint64(len(corpus)))]
		if len(other) > 0 && string(other) != string(out) {
			out = crossoverBytes(out, other, salt^0xC0FFEE, maxLen)
		}
	}
	// Pre-extract CmpLog-ish constants once per mutation (replay-stable, CPU-only).
	var cmpConsts [][]byte
	if len(corpus) > 0 {
		cmpConsts = ExtractCmpConstants(corpus...)
	}
	rounds := havocStackDepth(stage, salt)
	for i := 0; i < rounds; i++ {
		mix := splitmix64(salt ^ uint64(s) ^ uint64(i)*0x517cc1b727220a95)
		switch havocOpPick(mix) {
		case 0:
			idx := int(mix % uint64(len(out)))
			out[idx] ^= byte(1 << (mix % 8))
		case 1:
			if len(out) < growCap {
				out = append(out, byte(mix>>8))
			}
		case 2:
			if len(out) > 1 {
				out = out[:len(out)-1]
			}
		case 3:
			op := dictPickFrom(mix, dict)
			idx := int(mix>>8) % (len(out) + 1)
			if idx >= len(out) {
				out = append(out, op)
			} else {
				out[idx] = op
			}
		case 4:
			if len(out) >= 2 {
				start := int(mix % uint64(len(out)-1))
				n := 1 + int(mix>>16)%4
				for j := 0; j < n && start+j < len(out); j++ {
					out[start+j] = dictPickFrom(mix>>(uint(8*j)%56), dict)
				}
			}
		case 5:
			if mix%2 == 0 && len(out) < growCap {
				chunk := out
				if len(chunk) > 8 {
					chunk = chunk[:8]
				}
				out = append(out, chunk...)
			} else if len(out) > 4 {
				out = out[:len(out)/2]
			}
		case 6:
			idx := int(mix % uint64(len(out)))
			out[idx] += byte(mix >> 24)
		case 7:
			if len(out) > 0 {
				idx := int(mix % uint64(len(out)))
				vals := Interesting8()
				out[idx] = vals[int(mix>>8)%len(vals)]
			}
		case 8:
			if len(out) >= 2 {
				idx := int(mix % uint64(len(out)-1))
				vals := Interesting16LE()
				writeU16LE(out, idx, vals[int(mix>>16)%len(vals)])
			}
		case 9:
			if len(out) >= 4 {
				idx := int(mix % uint64(len(out)-3))
				vals := Interesting32LE()
				writeU32LE(out, idx, vals[int(mix>>16)%len(vals)])
			}
		case 10:
			if len(out) >= 1 {
				idx := int(mix % uint64(len(out)))
				arithAdd8(out, idx, int8((mix>>8)&0xff)-64)
			}
		case 11:
			if len(out) >= 2 {
				idx := int(mix % uint64(len(out)-1))
				arithAdd16LE(out, idx, int16((mix>>8)&0xffff)-128)
			}
		case 12:
			if tok := dictTokenAt(dict, mix); len(tok) > 0 {
				idx := int(mix>>16) % (len(out) + 1)
				out = insertToken(out, idx, tok, maxLen)
			}
		case 13:
			if tok := dictTokenAt(dict, mix); len(tok) > 0 {
				idx := int(mix>>16) % len(out)
				out = overwriteWithToken(out, idx, tok)
			}
		case 14:
			if len(out) >= 4 && len(out)*2 <= maxLen {
				start := int(mix % uint64(len(out)/2))
				n := 1 + int(mix>>8)%8
				if start+n <= len(out) {
					out = append(out, out[start:start+n]...)
				}
			}
		case 15:
			if len(out) > 8 {
				start := int(mix % uint64(len(out)-4))
				end := start + 2 + int(mix>>8)%6
				if end > len(out) {
					end = len(out)
				}
				if end > start {
					out = append(out[:start], out[end:]...)
				}
			}
		case 16: // big-endian interesting 16
			if len(out) >= 2 {
				idx := int(mix % uint64(len(out)-1))
				vals := Interesting16BE()
				writeU16BE(out, idx, vals[int(mix>>16)%len(vals)])
			}
		case 17: // big-endian interesting 32
			if len(out) >= 4 {
				idx := int(mix % uint64(len(out)-3))
				vals := Interesting32BE()
				writeU32BE(out, idx, vals[int(mix>>16)%len(vals)])
			}
		case 18: // arith32 LE
			if len(out) >= 4 {
				idx := int(mix % uint64(len(out)-3))
				arithAdd32LE(out, idx, int32((mix>>8)&0xffff)-256)
			}
		case 19: // structure smash (JSON/XML-ish)
			out = structureSmash(out, mix, maxLen)
		case 20: // corpus crossover mid-havoc
			if len(corpus) > 0 {
				other := corpus[int(mix%uint64(len(corpus)))]
				if len(other) > 0 {
					out = crossoverBytes(out, other, mix, maxLen)
				}
			}
		case 21: // random byte insert burst
			if len(out) < growCap {
				n := 1 + int(mix%4)
				for j := 0; j < n && len(out) < growCap && len(out) < maxLen; j++ {
					idx := int((mix >> uint(8*(j+1))) % uint64(len(out)+1))
					b := byte(mix >> uint(8*j))
					out = insertToken(out, idx, []byte{b}, maxLen)
				}
			}
		case 22: // shuffle small window
			if len(out) >= 4 {
				start := int(mix % uint64(len(out)-3))
				a, b := start, start+1+int(mix>>8)%3
				if b < len(out) {
					out[a], out[b] = out[b], out[a]
				}
			}
		case 23: // varint / length-prefix tamper
			idx := int(mix % uint64(len(out)))
			out = patchVarintLE(out, idx, mix, maxLen)
		case 24: // invalid UTF-8 splice
			idx := int(mix % uint64(len(out)+1))
			out = insertInvalidUTF8(out, idx, mix, maxLen)
		case 25: // rotate byte block
			out = rotateBlock(out, mix)
		case 26: // parity flip run
			out = parityFlip(out, mix)
		case 27: // triple crossover (parent A + B + C)
			if len(corpus) >= 3 {
				a := corpus[int(mix%uint64(len(corpus)))]
				b := corpus[int((mix>>8)%uint64(len(corpus)))]
				c := corpus[int((mix>>16)%uint64(len(corpus)))]
				tmp := crossoverBytes(a, b, mix, maxLen)
				out = crossoverBytes(tmp, c, mix>>24, maxLen)
			}
		case 28: // expand all bytes to 0xFF run
			if len(out) >= 2 && len(out) < maxLen {
				idx := int(mix % uint64(len(out)))
				out = insertToken(out, idx, []byte{0xff, 0xff, 0xff}, maxLen)
			}
		case 29: // zero-fill window
			if len(out) >= 4 {
				start := int(mix % uint64(len(out)-3))
				for j := 0; j < 4 && start+j < len(out); j++ {
					out[start+j] = 0
				}
			}
		case 30: // duplicate half (amplification)
			if len(out)*2 <= maxLen && len(out) >= 2 {
				out = append(out, out...)
			}
		case 31: // set length-ish prefix (common parser footgun)
			if len(out) >= 4 {
				claimed := uint32(mix & 0xffff)
				if mix&1 == 0 {
					writeU32LE(out, 0, claimed)
				} else {
					writeU32BE(out, 0, claimed)
				}
			}
		// --- v2.6 ops (32–47) ---
		case 32:
			out = reverseWindow(out, mix)
		case 33:
			out = swapEndian64(out, mix)
		case 34:
			out = sieveReplaceByte(out, mix)
		case 35:
			idx := int(mix % uint64(len(out)+1))
			out = insertFootgunToken(out, idx, mix, maxLen)
		case 36:
			out = caseFlipASCII(out, mix)
		case 37:
			out = repeatTokenBurst(out, mix, dict, maxLen)
		case 38:
			out = adjacentBitflip(out, mix)
		case 39:
			out = chunkLengthMismatch(out, mix)
		case 40:
			out = spliceCorpusSlice(out, corpus, mix, maxLen)
		case 41: // second structure smash pass (different mix)
			out = structureSmash(out, mix^0xA5A5A5A5, maxLen)
		case 42: // insert CRLF + fold
			idx := int(mix % uint64(len(out)+1))
			out = insertToken(out, idx, []byte{'\r', '\n', ' ', '\t'}, maxLen)
		case 43: // overwrite with interesting8 run
			if len(out) >= 2 {
				vals := Interesting8()
				start := int(mix % uint64(len(out)-1))
				for j := 0; j < 3 && start+j < len(out); j++ {
					out[start+j] = vals[int((mix>>uint(8*j))%uint64(len(vals)))]
				}
			}
		case 44: // truncate to power-of-two-ish
			if len(out) > 8 {
				keep := 4 + int(mix%uint64(len(out)/2))
				if keep < len(out) {
					out = out[:keep]
				}
			}
		case 45: // pad with 0x00 / 0x20 alternating
			if len(out) < growCap && len(out) < maxLen {
				n := 1 + int(mix%8)
				pad := make([]byte, 0, n)
				for j := 0; j < n; j++ {
					if j%2 == 0 {
						pad = append(pad, 0x00)
					} else {
						pad = append(pad, 0x20)
					}
				}
				idx := int((mix >> 8) % uint64(len(out)+1))
				out = insertToken(out, idx, pad, maxLen)
			}
		case 46: // double crossover with two corpus parents (ordered)
			if len(corpus) >= 2 {
				a := corpus[int(mix%uint64(len(corpus)))]
				b := corpus[int((mix>>16)%uint64(len(corpus)))]
				out = crossoverBytes(crossoverBytes(out, a, mix, maxLen), b, mix>>8, maxLen)
			}
		case 47: // structure smash + footgun
			out = structureSmash(out, mix, maxLen)
			idx := int((mix >> 4) % uint64(len(out)+1))
			out = insertFootgunToken(out, idx, mix>>8, maxLen)
		// --- v2.7 ops (48–63) ---
		case 48:
			out = nestBraces(out, mix, maxLen)
		case 49:
			out = utf16LEExpand(out, mix, maxLen)
		case 50:
			out = bitReverseByte(out, mix)
		case 51:
			out = setInterestingMagnitude(out, mix)
		case 52:
			idx := int(mix % uint64(len(out)+1))
			out = insertToken(out, idx, []byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"), maxLen)
		case 53:
			out = protobufWireSmash(out, mix)
		case 54:
			out = injectFloatBits(out, mix)
		case 55:
			out = deltaAdjacent(out, mix)
		case 56:
			idx := int(mix % uint64(len(out)+1))
			out = insertToken(out, idx, []byte("/*x*/ //y\n"), maxLen)
		case 57:
			out = wrapLengthFrame(out, mix, maxLen)
		case 58:
			out = shuffleWindow8(out, mix)
		case 59:
			idx := int(mix % uint64(len(out)+1))
			out = insertToken(out, idx, []byte{0xe2, 0x80, 0x8e, 0xe2, 0x80, 0x8f}, maxLen) // LTR/RTL
		case 60:
			out = corpusMaskMerge(out, corpus, mix, maxLen)
		case 61:
			out = arithEveryNth(out, mix)
		case 62:
			out = structureSmash(out, mix^0x5a5a5a5a, maxLen)
			out = insertFootgunToken(out, int(mix%uint64(len(out)+1)), mix>>3, maxLen)
		case 63: // JSON number overflow / nested smash combo
			idx := int(mix % uint64(len(out)+1))
			out = insertToken(out, idx, []byte("1e309"), maxLen)
			out = nestBraces(out, mix>>8, maxLen)
		// --- v2.8 ops (64–79): CmpLog-inspired + deeper shape churn ---
		case 64:
			out = cmpReplaceWithInteresting(out, mix)
			if mix&8 != 0 {
				out = cmpInsertBoundary(out, mix>>4, maxLen)
			}
		case 65:
			out = cmpArithTowardInteresting(out, mix)
			if mix&16 != 0 && len(out) < growCap {
				out = insertToken(out, int(mix%uint64(len(out)+1)), []byte{byte(mix >> 24)}, maxLen)
			}
		case 66:
			out = cmpSwapWithCorpusConst(out, cmpConsts, mix)
			if mix&32 != 0 {
				out = structureSmash(out, mix>>6, maxLen)
			}
		case 67:
			out = cmpXorWindow(out, mix)
			if mix&64 != 0 {
				out = reverseWindow(out, mix>>8)
			}
		case 68:
			out = cmpInsertBoundary(out, mix, maxLen)
			out = nestBraces(out, mix>>5, maxLen)
		case 69:
			out = spliceCmpConst(out, corpus, mix, maxLen)
			if len(corpus) > 0 {
				out = spliceCorpusSlice(out, corpus, mix>>7, maxLen)
			}
		case 70:
			out = repeatRareNibble(out, mix)
			out = adjacentBitflip(out, mix>>3)
		case 71:
			idx := int(mix % uint64(len(out)+1))
			out = insertUTF8Overlong(out, idx, mix, maxLen)
			out = insertInvalidUTF8(out, idx, mix>>4, maxLen)
		case 72:
			out = widenThenNarrow(out, mix, maxLen)
			out = chunkLengthMismatch(out, mix>>9)
		case 73:
			out = interleaveCorpus(out, corpus, mix, maxLen)
			out = crossoverBytes(out, out, mix^0xF00D, maxLen) // self-skew cut
		case 74: // dual CmpLog: replace then arith + boundary
			out = cmpReplaceWithInteresting(out, mix)
			out = cmpArithTowardInteresting(out, mix>>3)
			out = cmpInsertBoundary(out, mix>>11, maxLen)
		case 75: // corpus const overwrite + boundary insert + smash
			out = cmpSwapWithCorpusConst(out, cmpConsts, mix)
			out = cmpInsertBoundary(out, mix>>5, maxLen)
			out = structureSmash(out, mix>>13, maxLen)
		case 76: // XOR window then interesting magnitude + footgun
			out = cmpXorWindow(out, mix)
			out = setInterestingMagnitude(out, mix>>7)
			out = insertFootgunToken(out, int(mix%uint64(len(out)+1)), mix>>15, maxLen)
		case 77: // interleave + structure smash + nest
			out = interleaveCorpus(out, corpus, mix, maxLen)
			out = structureSmash(out, mix^0xC0C0C0C0, maxLen)
			out = nestBraces(out, mix>>4, maxLen)
		case 78: // widen/narrow + footgun + utf16
			out = widenThenNarrow(out, mix, maxLen)
			out = insertFootgunToken(out, int(mix%uint64(len(out)+1)), mix>>4, maxLen)
			out = utf16LEExpand(out, mix>>8, maxLen)
		default: // 79 — CmpLog triple + havoc shape
			out = spliceCmpConst(out, corpus, mix, maxLen)
			out = cmpArithTowardInteresting(out, mix>>9)
			out = cmpInsertBoundary(out, mix>>17, maxLen)
			out = widenThenNarrow(out, mix>>21, maxLen)
		}
		if len(out) == 0 {
			out = []byte{byte(mix)}
		}
		if len(out) > maxLen {
			out = out[:maxLen]
		}
	}
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}
