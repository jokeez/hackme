package fuzzengine

import "testing"

func TestHavocOpWeightsSum(t *testing.T) {
	var sum int
	for _, w := range havocOpWeights {
		sum += int(w)
		if w == 0 {
			t.Fatal("every havoc op must stay reachable (weight >= 1)")
		}
	}
	if sum != havocOpWeightSum {
		t.Fatalf("havocOpWeights sum=%d want %d", sum, havocOpWeightSum)
	}
	if len(havocOpWeights) != HavocOpModulo {
		t.Fatalf("weights len=%d want HavocOpModulo=%d", len(havocOpWeights), HavocOpModulo)
	}
}
