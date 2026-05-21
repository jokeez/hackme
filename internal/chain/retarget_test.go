package chain

import "testing"

func TestClampPoHTargetModAvoidsMultipleOf7(t *testing.T) {
	// User-reported stuck M: 82508251 = 7×11786893 — no n satisfies 7n+13≡0 (mod M).
	bad := uint64(82_508_251)
	fixed := ClampPoHTargetMod(bad)
	if fixed%7 == 0 {
		t.Fatalf("Clamp(%d)=%d still divisible by 7", bad, fixed)
	}
	if fixed < pohRetargetMinMod || fixed > pohRetargetMaxMod {
		t.Fatalf("out of bounds: %d", fixed)
	}
	if fixed != bad+1 {
		t.Fatalf("expected %d+1, got %d", bad, fixed)
	}
}

func TestPohHitExistsNearZeroAfterSanitize(t *testing.T) {
	m := ClampPoHTargetMod(82_508_251)
	var found bool
	for n := uint64(0); n < m*3; n++ {
		if PohEval(n)%m == 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no hit in [0,3M) for M=%d", m)
	}
}

func TestActivePoHTaskKindForIndex(t *testing.T) {
	if got := ActivePoHTaskKindForIndex(PoHFormulaV2ActivationHeight - 1); got != TaskKindSyntheticPoH {
		t.Fatalf("before activation got=%s", got)
	}
	if got := ActivePoHTaskKindForIndex(PoHFormulaV2ActivationHeight); got != TaskKindSyntheticPoHV2 {
		t.Fatalf("at activation got=%s", got)
	}
}

func TestPohEvalForIndexMatchesKind(t *testing.T) {
	n := uint64(123456789)
	v1 := PohEvalForIndex(PoHFormulaV2ActivationHeight-1, n)
	v2 := PohEvalForIndex(PoHFormulaV2ActivationHeight, n)
	if v1 != PohEvalByKind(TaskKindSyntheticPoH, n) {
		t.Fatalf("v1 mismatch")
	}
	if v2 != PohEvalByKind(TaskKindSyntheticPoHV2, n) {
		t.Fatalf("v2 mismatch")
	}
	if v1 == v2 {
		t.Fatalf("expected v1 and v2 eval to differ for nonce=%d", n)
	}
}

func TestRetargetMicroStepRespondsToFastAndSlowBlocks(t *testing.T) {
	prev := uint64(1_000_000)
	fast := RetargetMicroStep(prev, 3, PoHRetargetTargetSec)   // much faster than 30s target
	slow := RetargetMicroStep(prev, 180, PoHRetargetTargetSec) // much slower than target
	if fast <= prev {
		t.Fatalf("expected fast block to increase difficulty mod, got prev=%d fast=%d", prev, fast)
	}
	if slow >= prev {
		t.Fatalf("expected slow block to decrease difficulty mod, got prev=%d slow=%d", prev, slow)
	}
}

func TestRetargetWindowStableAtTargetBlockTime(t *testing.T) {
	prev := uint64(5_000_000)
	ideal := PoHRetargetWindowBlocks * PoHRetargetTargetSec
	next := RetargetWindow(prev, ideal, ideal)
	if next != prev {
		t.Fatalf("on-target window should keep M: prev=%d next=%d", prev, next)
	}
}

func TestRetargetWindowHarderOnHashrateSpike(t *testing.T) {
	prev := uint64(2_000_000)
	ideal := PoHRetargetWindowBlocks * PoHRetargetTargetSec
	// Blocks arrived 5x faster than target → difficulty up.
	fast := RetargetWindow(prev, ideal/5, ideal)
	if fast <= prev {
		t.Fatalf("fast blocks should increase M: prev=%d fast=%d", prev, fast)
	}
}

func TestRetargetMicroStepIsBounded(t *testing.T) {
	prev := uint64(1_000_000)
	up := RetargetMicroStep(prev, 1, PoHRetargetTargetSec)
	down := RetargetMicroStep(prev, 10_000, PoHRetargetTargetSec)
	maxUp := uint64(float64(prev)*poHRetargetMicroMaxStepUp + 1)
	minDown := uint64(float64(prev)*poHRetargetMicroMaxStepDown - 1)
	if up > maxUp {
		t.Fatalf("unexpected micro up step: got=%d max=%d", up, maxUp)
	}
	if down < minDown {
		t.Fatalf("unexpected micro down step: got=%d min=%d", down, minDown)
	}
}
