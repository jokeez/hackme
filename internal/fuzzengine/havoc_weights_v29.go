package fuzzengine

// Soft havoc-op weight table (v2.9): static, salt-keyed via mix.
// Biases CmpLog / splice / dict without online learning. Every op stays reachable (w≥1).
// Sum is fixed so pick is pure: mix % sum → cumulative bucket.

const havocOpWeightSum = 256

// havocOpWeights length must equal HavocOpModulo (80).
var havocOpWeights = [HavocOpModulo]uint8{
	// 0–15: basic + dict-ish (dict 3, interesting 12–13 raised)
	2, 2, 2, 5, 2, 2, 2, 2, 2, 2, 2, 2, 5, 5, 2, 2, // 41
	// 16–31: arith / structure / crossover (20, 27 raised)
	2, 2, 2, 3, 7, 2, 2, 2, 3, 2, 2, 7, 2, 2, 2, 2, // 44
	// 32–47: v2.6 footguns / splice (37 dict burst, 40 splice, 46 ordered)
	2, 2, 2, 3, 2, 5, 2, 2, 7, 2, 2, 2, 2, 2, 7, 3, // 47
	// 48–63: v2.7 shape
	2, 2, 2, 3, 2, 2, 2, 2, 2, 2, 3, 2, 3, 3, 2, 3, // 37
	// 64–79: CmpLog-inspired (biased up)
	6, 6, 6, 5, 5, 7, 4, 4, 4, 6, 6, 6, 5, 5, 5, 7, // 88
}

func init() {
	var sum int
	for _, w := range havocOpWeights {
		sum += int(w)
	}
	if sum != havocOpWeightSum {
		panic("havocOpWeights sum must equal havocOpWeightSum")
	}
}

// havocOpPick selects an op in [0, HavocOpModulo) from mix using the static weight table.
func havocOpPick(mix uint64) int {
	r := mix % uint64(havocOpWeightSum)
	var acc uint64
	for op := 0; op < HavocOpModulo; op++ {
		acc += uint64(havocOpWeights[op])
		if r < acc {
			return op
		}
	}
	return HavocOpModulo - 1
}
