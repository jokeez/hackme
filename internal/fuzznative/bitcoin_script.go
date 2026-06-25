package fuzznative

// Go ports of tasks/sources/security/upstream/bitcoin_*.c guard logic for native repro.

const (
	maxScriptElementSize = 520
	maxOpcode            = 0xb9
	opPushData1          = 0x4c
	opPushData2          = 0x4d
	opPushData4          = 0x4e
	maxStackSize         = 1000
	maxOpsPerScript      = 201
	op16                 = 0x60
	opNOP                = 0x61
	maxMoney             = int64(21_000_000) * 100_000_000
)

func inputU64(input []byte) uint64 {
	var u uint64
	for i := 0; i < 8; i++ {
		var b byte
		if i < len(input) {
			b = input[i]
		}
		u |= uint64(b) << (8 * i)
	}
	return u
}

func bitcoinGetScriptOp(pc *int, end int, script []byte) (opcode byte, pushLen uint, ok bool) {
	if *pc >= end {
		return 0, 0, false
	}
	if end-*pc < 1 {
		return 0, 0, false
	}
	pos := *pc
	opcode = script[pos]
	pos++
	if opcode <= opPushData4 {
		var nSize uint
		switch {
		case opcode < opPushData1:
			nSize = uint(opcode)
		case opcode == opPushData1:
			if end-pos < 1 {
				return 0, 0, false
			}
			nSize = uint(script[pos])
			pos++
		case opcode == opPushData2:
			if end-pos < 2 {
				return 0, 0, false
			}
			nSize = uint(script[pos]) | uint(script[pos+1])<<8
			pos += 2
		case opcode == opPushData4:
			if end-pos < 4 {
				return 0, 0, false
			}
			nSize = uint(script[pos]) | uint(script[pos+1])<<8 |
				uint(script[pos+2])<<16 | uint(script[pos+3])<<24
			pos += 4
		}
		if end-pos < int(nSize) {
			return 0, 0, false
		}
		pushLen = nSize
		pos += int(nSize)
	}
	*pc = pos
	return opcode, pushLen, true
}

func scriptBytes8(input []byte) [8]byte {
	u := inputU64(input)
	var out [8]byte
	for i := 0; i < 8; i++ {
		out[i] = byte(u >> (8 * i))
	}
	return out
}

func scriptBytes6High(input []byte) [6]byte {
	u := inputU64(input)
	var out [6]byte
	for i := 0; i < 6; i++ {
		out[i] = byte(u >> (8 * (i + 3)))
	}
	return out
}

func scriptBytes6Tail(input []byte) [6]byte {
	u := inputU64(input)
	var out [6]byte
	for i := 0; i < 6; i++ {
		out[i] = byte(u >> (8 * (i + 1)))
	}
	return out
}

func evalBitcoinGetScriptOp(input []byte) int {
	script := scriptBytes8(input)
	pc := 0
	end := 8
	for pc < end {
		opcode, pushLen, ok := bitcoinGetScriptOp(&pc, end, script[:])
		if !ok {
			return 1
		}
		if opcode <= opPushData4 && pushLen > maxScriptElementSize {
			return 1
		}
	}
	return 0
}

func evalBitcoinHasValidOps(input []byte) int {
	script := scriptBytes8(input)
	pc := 0
	end := 8
	for pc < end {
		opcode, itemSize, ok := bitcoinGetScriptOp(&pc, end, script[:])
		if !ok || opcode > maxOpcode || itemSize > maxScriptElementSize {
			return 1
		}
	}
	return 0
}

func evalBitcoinEvalScriptPush(input []byte) int {
	script := scriptBytes8(input)
	pc := 0
	end := 8
	for pc < end {
		_, pushLen, ok := bitcoinGetScriptOp(&pc, end, script[:])
		if !ok {
			return 1
		}
		if pushLen > maxScriptElementSize {
			return 1
		}
	}
	return 0
}

func evalBitcoinWitnessStack(input []byte) int {
	u := inputU64(input)
	for i := 0; i < 4; i++ {
		elem := uint((u >> (16 * i)) & 0xffff)
		if elem > maxScriptElementSize {
			return 1
		}
	}
	return 0
}

func evalBitcoinEvalScriptStack(input []byte) int {
	u := inputU64(input)
	mainSz := uint(u & 0xfff)
	altSz := uint((u >> 12) & 0xfff)
	stackDepth := mainSz + altSz

	script := scriptBytes6High(input)
	pc := 0
	end := 6
	for pc < end {
		opcode, _, ok := bitcoinGetScriptOp(&pc, end, script[:])
		if !ok {
			return 1
		}
		if opcode <= opPushData4 {
			stackDepth++
			if stackDepth > maxStackSize {
				return 1
			}
		}
	}
	if stackDepth > maxStackSize {
		return 1
	}
	return 0
}

func evalBitcoinEvalScriptOpCount(input []byte) int {
	u := inputU64(input)
	repeat := uint(u & 0xff)
	nOpCount := uint(0)

	for i := uint(0); i < repeat; i++ {
		opcode := byte(opNOP)
		if opcode > op16 {
			nOpCount++
			if nOpCount > maxOpsPerScript {
				return 1
			}
		}
	}

	script := scriptBytes6Tail(input)
	pc := 0
	end := 6
	for pc < end {
		opcode, _, ok := bitcoinGetScriptOp(&pc, end, script[:])
		if !ok {
			return 1
		}
		if opcode > op16 {
			nOpCount++
			if nOpCount > maxOpsPerScript {
				return 1
			}
		}
	}
	return 0
}

func evalBitcoinTxCheckMoneyRange(input []byte) int {
	u := inputU64(input)
	v0 := int64(int32(u & 0xffffffff))
	v1 := int64(int32((u >> 32) & 0xffffffff))
	if !moneyRange(v0) {
		return 1
	}
	if !moneyRange(v1) {
		return 1
	}
	if !moneyRange(v0 + v1) {
		return 1
	}
	return 0
}

func moneyRange(v int64) bool {
	return v >= 0 && v <= maxMoney
}
