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

// MutateBytesForConfig applies byte mutations with optional pack mutator_dict.
func MutateBytesForConfig(base []byte, stage MutationStage, salt uint64, maxLen int, cfg map[string]any) []byte {
	return mutateBytesWithDict(base, stage, salt, maxLen, ParseMutatorDict(cfg))
}

func mutateBytesWithDict(base []byte, stage MutationStage, salt uint64, maxLen int, dict []byte) []byte {
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
	rounds := 1 + int((salt+uint64(s))%4)
	for i := 0; i < rounds; i++ {
		mix := splitmix64(salt ^ uint64(s) ^ uint64(i)*0x517cc1b727220a95)
		switch mix % 8 {
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
		default:
			idx := int(mix % uint64(len(out)))
			out[idx] += byte(mix >> 24)
		}
	}
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}
