package chain

import "math"

// DefaultPoHTargetMod is the initial PoH modulus M after genesis (eval(nonce)%M==0).
const DefaultPoHTargetMod uint64 = 1_000_000

// PoHRetargetTargetSec is the desired average wall-clock seconds between PoH blocks.
const PoHRetargetTargetSec int64 = 30

// PoHRetargetWindowBlocks — retarget runs when the new tip height is a positive multiple
// of this value (e.g. 5, 10, 15…), comparing wall time across the last window of blocks.
const PoHRetargetWindowBlocks int64 = 5

const (
	// PoHTargetMinMod is the lower bound for PoH modulus.
	PoHTargetMinMod uint64 = 251
	// PoHTargetMaxMod is the upper bound for PoH modulus.
	// Raised to support larger aggregate hashrate before saturation.
	PoHTargetMaxMod uint64 = 10_000_000_000_000

	// Per-window retarget change limits to reduce oscillations.
	poHRetargetMaxStepUp   = 4.0
	poHRetargetMaxStepDown = 1.0 / 4.0

	// Per-block micro-adjust limits for faster adaptation between full windows.
	// Helps absorb sudden hashrate spikes/drops without waiting full retarget window.
	poHRetargetMicroMaxStepUp   = 1.35
	poHRetargetMicroMaxStepDown = 1.0 / 1.35

	pohRetargetMinMod = PoHTargetMinMod
	pohRetargetMaxMod = PoHTargetMaxMod
)

// PoHFormulaV2ActivationHeight is a deterministic chain-height gate for baseline profile v2.
// Until this height, baseline remains synthetic_poh_v1 to preserve compatibility.
const PoHFormulaV2ActivationHeight uint64 = 2_102_400

// ActivePoHTaskKindForIndex selects the baseline PoH profile for a target block height.
func ActivePoHTaskKindForIndex(index uint64) TaskKind {
	if index >= PoHFormulaV2ActivationHeight {
		return TaskKindSyntheticPoHV2
	}
	return TaskKindSyntheticPoH
}

// PoHFormulaLabelForKind returns a human-readable formula label for block payloads/UI.
func PoHFormulaLabelForKind(kind TaskKind) string {
	switch kind {
	case TaskKindSyntheticPoHV2:
		return "eval_v2(n)=mix64(11n+17)"
	default:
		return "eval_v1(n)=n*7+13"
	}
}

// PoHFormulaLabelForIndex returns the active formula label for a target block height.
func PoHFormulaLabelForIndex(index uint64) string {
	return PoHFormulaLabelForKind(ActivePoHTaskKindForIndex(index))
}

// PohEval is the v1 baseline formula for compatibility with older call sites/tests.
// It matches embedded WASM eval(i64)->i64: n*7+13.
func PohEval(n uint64) uint64 {
	return n*7 + 13
}

// PohEvalV2 is the next baseline profile.
func PohEvalV2(n uint64) uint64 {
	x := n*11 + 17
	x ^= x >> 23
	x *= 0x9e3779b185ebca87
	x ^= x >> 29
	return x + 0x7f4a7c15
}

// PohEvalByKind evaluates nonce with a specific baseline profile.
func PohEvalByKind(kind TaskKind, n uint64) uint64 {
	switch kind {
	case TaskKindSyntheticPoHV2:
		return PohEvalV2(n)
	default:
		return PohEval(n)
	}
}

// PohEvalForIndex evaluates nonce using the baseline profile active at block index.
func PohEvalForIndex(index, n uint64) uint64 {
	return PohEvalByKind(ActivePoHTaskKindForIndex(index), n)
}

// RetargetWindow returns the next modulus after a completed window of blocks.
// M_next = M_prev * T_ideal / T_actual (then clamped by caller):
//   - blocks faster than target (T_actual < T_ideal) → larger M (harder);
//   - slower → smaller M (easier).
//
// T_ideal = windowBlocks * targetSecPerBlock (e.g. 5 * 30s = 150s).
func RetargetWindow(prevMod uint64, actualSec, idealSec int64) uint64 {
	if prevMod < pohRetargetMinMod {
		prevMod = pohRetargetMinMod
	}
	if prevMod > pohRetargetMaxMod {
		prevMod = pohRetargetMaxMod
	}
	if idealSec < 1 {
		idealSec = 1
	}
	act := float64(actualSec)
	if actualSec < 1 {
		act = 1
	}
	ratio := float64(idealSec) / act
	// Limit per-window changes for smoother network adaptation.
	const maxRatio = poHRetargetMaxStepUp
	const minRatio = poHRetargetMaxStepDown
	if ratio > maxRatio {
		ratio = maxRatio
	}
	if ratio < minRatio {
		ratio = minRatio
	}
	nextF := float64(prevMod) * ratio
	if math.IsNaN(nextF) || math.IsInf(nextF, 0) {
		return pohRetargetMaxMod
	}
	if nextF >= float64(pohRetargetMaxMod) {
		return pohRetargetMaxMod
	}
	if nextF <= float64(pohRetargetMinMod) {
		return pohRetargetMinMod
	}
	return uint64(nextF + 0.5)
}

// RetargetMicroStep applies a bounded per-block adjustment using the latest block interval.
// This complements RetargetWindow and improves reaction speed to sudden hashrate shifts.
func RetargetMicroStep(prevMod uint64, actualSec, targetSec int64) uint64 {
	if prevMod < pohRetargetMinMod {
		prevMod = pohRetargetMinMod
	}
	if prevMod > pohRetargetMaxMod {
		prevMod = pohRetargetMaxMod
	}
	if targetSec < 1 {
		targetSec = 1
	}
	act := float64(actualSec)
	if actualSec < 1 {
		act = 1
	}
	ratio := float64(targetSec) / act
	if ratio > poHRetargetMicroMaxStepUp {
		ratio = poHRetargetMicroMaxStepUp
	}
	if ratio < poHRetargetMicroMaxStepDown {
		ratio = poHRetargetMicroMaxStepDown
	}
	nextF := float64(prevMod) * ratio
	if math.IsNaN(nextF) || math.IsInf(nextF, 0) {
		return pohRetargetMaxMod
	}
	if nextF >= float64(pohRetargetMaxMod) {
		return pohRetargetMaxMod
	}
	if nextF <= float64(pohRetargetMinMod) {
		return pohRetargetMinMod
	}
	return uint64(nextF + 0.5)
}

// ClampPoHTargetMod keeps a modulus within the allowed PoH retarget bounds and
// enforces solvability for the built-in eval(n)=7n+13 (uint64 / WASM i64).
//
// We need ∃n with (7n+13)≡0 (mod M), i.e. gcd(7,M)|13. Since 7∤13, M must not
// be divisible by 7; otherwise no nonce satisfies the PoH rule (retarget can
// otherwise pick such M, e.g. 82508251 = 7×11786893).
func ClampPoHTargetMod(p uint64) uint64 {
	if p < pohRetargetMinMod {
		p = pohRetargetMinMod
	} else if p > pohRetargetMaxMod {
		p = pohRetargetMaxMod
	}
	return sanitizePoHTargetMod7n13(p)
}

// IsPoHTargetModAtCap reports whether M is at the configured upper bound.
func IsPoHTargetModAtCap(m uint64) bool {
	return ClampPoHTargetMod(m) >= pohRetargetMaxMod
}

// sanitizePoHTargetMod7n13 nudges M off multiples of 7 while staying in bounds.
func sanitizePoHTargetMod7n13(m uint64) uint64 {
	if m%7 != 0 {
		return m
	}
	if m < pohRetargetMaxMod {
		return m + 1 // m≡0 (mod 7) ⇒ m+1 ≢ 0 (mod 7)
	}
	if m > pohRetargetMinMod {
		return m - 1
	}
	return pohRetargetMinMod
}
