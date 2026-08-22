package fuzzengine

// MutationStage selects deterministic bit-flip (0..63) or havoc (64+).
type MutationStage int

const (
	StageDeterministicMax = 64
	StageHavocBase        = 64
)

var scriptOpcodeDict = []byte{
	0x00, 0x4c, 0x4d, 0x52, 0x63, 0x68, 0x76, 0xac,
	0x01, 0x02, 0x03, 0x4e, 0x82, 0x83,
}

func dictPick(mix uint64) byte {
	return scriptOpcodeDict[mix%uint64(len(scriptOpcodeDict))]
}

// MutateInput applies staged mutation to a base seed input (lab parity sketch).
func MutateInput(base uint64, stage MutationStage, salt uint64) uint64 {
	s := int(stage)
	if s < StageDeterministicMax {
		bit := uint(s % 64)
		return base ^ (uint64(1) << bit)
	}
	out := base
	rounds := 1 + int((salt+uint64(s))%4)
	for i := 0; i < rounds; i++ {
		mix := splitmix64(base ^ salt ^ uint64(s) ^ uint64(i)*0x517cc1b727220a95)
		switch mix % 8 {
		case 0:
			out ^= uint64(1) << (mix % 64)
		case 1:
			out += mix | 1
		case 2:
			out ^= mix
		case 3:
			out = (out << 1) | (out >> 63)
		case 4:
			low := byte(mix)
			shift := (mix >> 8) % 56
			mask := uint64(low) << shift
			out ^= mask
		case 5:
			out = splitmix64(out)
		case 6:
			out = mutateInputDictionary(out, mix)
		case 7:
			op, itemID, qty := WasmCheckInputParts(out)
			_ = qty
			out = PackWasmCheckInput(op^int(mix&0xff), itemID^int(mix>>8&0xffff), int64(mix>>24))
		}
	}
	return out
}

func mutateInputDictionary(base uint64, mix uint64) uint64 {
	op, item, qty := WasmCheckInputParts(base)
	switch mix % 5 {
	case 0:
		op = int(scriptOpcodeDict[mix%uint64(len(scriptOpcodeDict))])
	case 1:
		item = int(mix & 0xffff)
	case 2:
		qty = int64(mix)
	case 3:
		qty = int64(0xffff_ffff) ^ int64(mix&0xffff)
	case 4:
		item = int(mix>>16) & 0xff
	}
	return PackWasmCheckInput(op, item, qty)
}

// PackWasmCheckInput builds the legacy uint64 layout used by check() guards.
func PackWasmCheckInput(opType, itemID int, quantity int64) uint64 {
	var u uint64
	u |= uint64(opType & 0xff)
	u |= uint64(itemID&0xffff) << 8
	u |= uint64(quantity) << 24
	return u
}

// MutationsForSeedCapped returns how many mutations one lab run would schedule for a seed.
func MutationsForSeedCapped(energy, cap int) int {
	if cap <= 0 {
		cap = 8
	}
	n := 1 + energy/2
	if n > cap {
		return cap
	}
	if n < 1 {
		return 1
	}
	return n
}
