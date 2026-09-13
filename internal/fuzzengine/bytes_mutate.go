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
func MutateBytesForHunt(base []byte, stage MutationStage, salt uint64, maxLen int, cfg map[string]any, corpus [][]byte) []byte {
	dict := EffectiveMutatorDict(cfg, corpus)
	return mutateBytesWithDict(base, stage, salt, maxLen, dict, corpus)
}

// MutateBytesForConfig applies byte mutations with optional pack mutator_dict.
func MutateBytesForConfig(base []byte, stage MutationStage, salt uint64, maxLen int, cfg map[string]any) []byte {
	return mutateBytesWithDict(base, stage, salt, maxLen, ParseMutatorDict(cfg), nil)
}

// havocStackDepth returns how many stacked havoc ops to apply (AFL-like energy).
// Deterministic from stage+salt so coordinator replay stays stable.
func havocStackDepth(stage MutationStage, salt uint64) int {
	s := int(stage)
	rounds := 1 + int((salt+uint64(s))%4)
	if s >= StageHavocBase {
		extra := (s - StageHavocBase) / 2
		if extra > 6 {
			extra = 6
		}
		rounds += extra
		// Occasional deep stack for rare stages (still bounded).
		if (salt^uint64(s))%11 == 0 {
			rounds += 2
		}
	}
	if rounds < 1 {
		rounds = 1
	}
	if rounds > 16 {
		rounds = 16
	}
	return rounds
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
		idx := s % len(out)
		out[idx] ^= byte(1 << (salt % 8))
		if len(out) > maxLen {
			out = out[:maxLen]
		}
		return out
	}
	out := append([]byte(nil), base...)
	// Corpus crossover before havoc — fleet diversity (deterministic from salt).
	if len(corpus) >= 2 && (salt%13) == 0 {
		other := corpus[int((salt>>8)%uint64(len(corpus)))]
		if len(other) > 0 && string(other) != string(out) {
			out = crossoverBytes(out, other, salt^0xC0FFEE, maxLen)
		}
	}
	rounds := havocStackDepth(stage, salt)
	for i := 0; i < rounds; i++ {
		mix := splitmix64(salt ^ uint64(s) ^ uint64(i)*0x517cc1b727220a95)
		switch mix % 32 {
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
					idx := int((mix>>uint(8*(j+1))) % uint64(len(out)+1))
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
		default: // set length-ish prefix (common parser footgun)
			if len(out) >= 4 {
				claimed := uint32(mix & 0xffff)
				if mix&1 == 0 {
					writeU32LE(out, 0, claimed)
				} else {
					writeU32BE(out, 0, claimed)
				}
			}
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
