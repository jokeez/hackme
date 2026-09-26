package fuzzengine

import "strings"

// DeepHavocV210 reports opt-in v2.10 burst (on top of deep-v28).
func DeepHavocV210(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	if v, ok := cfg["havoc_deep_v210"]; ok {
		return truthyCFG(v)
	}
	p := strings.TrimSpace(strings.ToLower(toString(cfg["hunt_mutator_profile"])))
	switch p {
	case "v210", "deep_v210", "max":
		return true
	}
	return false
}

// EnableDeepHavocV210 sets v2.10 burst for new Hunt campaigns (implies deep-v28).
func EnableDeepHavocV210(cfg map[string]any) {
	if cfg == nil {
		return
	}
	EnableDeepHavocV28(cfg)
	if _, ok := cfg["havoc_deep_v210"]; !ok {
		cfg["havoc_deep_v210"] = true
	}
}

// v2.10 deep burst — applied when DeepHavocV210 (still stage+salt keyed).
func applyDeepV210Burst(buf []byte, stage MutationStage, salt uint64, maxLen int, dict []byte, corpus [][]byte) []byte {
	if len(buf) == 0 {
		return []byte{byte(salt)}
	}
	out := append([]byte(nil), buf...)
	s := int(stage)
	rounds := 2 + int((salt^uint64(s*29))%5)
	if rounds > 8 {
		rounds = 8
	}
	for i := 0; i < rounds; i++ {
		mix := splitmix64(salt ^ 0x210210210210210 ^ uint64(s) ^ uint64(i)*0x9e3779b97f4a7c15)
		switch mix % 10 {
		case 0:
			out = multiTokenDictBurst(out, mix, dict, maxLen)
		case 1:
			out = widenThenNarrow(out, mix, maxLen)
		case 2:
			out = interleaveCorpus(out, corpus, mix, maxLen)
		case 3:
			out = repeatRareNibble(out, mix)
		case 4:
			out = spliceCmpConst(out, corpus, mix, maxLen)
		case 5:
			out = insertUTF8Overlong(out, int(mix%uint64(len(out)+1)), mix, maxLen)
		case 6:
			out = lengthCascadeCorrupt(out, mix)
		case 7:
			out = splice3wayRoundRobin(out, corpus, mix, maxLen)
		case 8:
			out = interesting64Smash(out, mix)
		default:
			out = walkingNBitFlip(out, mix)
			out = caseFlipASCII(out, mix>>8)
		}
		if len(out) == 0 {
			out = []byte{byte(mix)}
		}
		if len(out) > maxLen {
			out = out[:maxLen]
		}
	}
	return out
}

func multiTokenDictBurst(buf []byte, mix uint64, dict []byte, maxLen int) []byte {
	toks := ParseDictTokens(dict)
	if len(toks) == 0 {
		toks = [][]byte{[]byte("null"), []byte("true"), []byte("{}"), []byte("[]")}
	}
	out := append([]byte(nil), buf...)
	n := 2 + int((mix>>4)%3)
	for i := 0; i < n; i++ {
		tok := toks[int((mix>>uint(8+i*5))%uint64(len(toks)))]
		idx := int((mix >> uint(16+i*3)) % uint64(len(out)+1))
		out = insertToken(out, idx, tok, maxLen)
	}
	return out
}
