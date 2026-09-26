package fuzzengine

import (
	"hash/fnv"
	"strings"
)

const (
	CoverageKindHuntStructural = "hunt_structural"
)

// CoverageFeedbackEnabled is true when structural/edge feedback should drive corpus energy.
func CoverageFeedbackEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return false
	}
	if v, ok := cfg["coverage_feedback_v1"]; ok {
		return cfgTruthy(v)
	}
	// Hunt L2 defaults structural feedback when corpus-guided.
	if strings.EqualFold(strings.TrimSpace(toString(cfg["hunt_corpus_guided"])), "true") ||
		toString(cfg["hunt_corpus_guided"]) == "1" {
		return true
	}
	k := strings.TrimSpace(strings.ToLower(CoverageKind(cfg)))
	return k == CoverageKindHuntStructural || k == CoverageKindWasmEdgeBitmap
}

func cfgTruthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(strings.ToLower(t))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case int:
		return t != 0
	case float64:
		return t != 0
	default:
		return false
	}
}

// StructuralFeatureScore returns a 0..255 score from byte input shape (JSON/XML/binary hints).
func StructuralFeatureScore(input []byte) int {
	if len(input) == 0 {
		return 0
	}
	score := len(input) % 257
	score += strings.Count(string(input), "{") * 3
	score += strings.Count(string(input), "[") * 2
	score += strings.Count(string(input), "<") * 2
	score += strings.Count(string(input), "\"") * 2
	score += strings.Count(string(input), "\x00") * 5
	score += strings.Count(string(input), "\\u") * 2
	if score > 255 {
		score = 255
	}
	return score
}

// CoverageBucketsStructural returns richer edge/path buckets for Hunt ASAN (no WASM bitmap).
func CoverageBucketsStructural(input []byte) (edgeBucket, pathBucket int) {
	if len(input) == 0 {
		return 0, 0
	}
	h := fnv.New64a()
	_, _ = h.Write(input)
	mix := h.Sum64()

	// Edge: structural regions (quartiles + feature score).
	regions := 4
	if len(input) < regions {
		regions = len(input)
	}
	edgeAcc := 0
	for i := 0; i < regions; i++ {
		start := (i * len(input)) / regions
		end := ((i + 1) * len(input)) / regions
		if end <= start {
			end = start + 1
		}
		if end > len(input) {
			end = len(input)
		}
		rh := fnv.New32a()
		_, _ = rh.Write(input[start:end])
		edgeAcc += int(rh.Sum32() % 64)
	}
	edgeAcc += StructuralFeatureScore(input)
	edgeBucket = edgeAcc % 257

	// Path: length class + token density + prefix hash.
	pathMix := mix ^ uint64(len(input)*9973)
	pathMix ^= uint64(strings.Count(string(input), ":")) << 8
	pathMix ^= uint64(strings.Count(string(input), ",")) << 16
	pathBucket = int(pathMix % 509)
	return edgeBucket, pathBucket
}

// BitmapEdgeCount returns approximate edge hits in a WASM/sandbox edge bitmap.
func BitmapEdgeCount(bitmap []byte) int {
	n := 0
	for _, c := range bitmap {
		if c != 0 {
			n++
		}
	}
	return n
}

// CorpusObserveBoostWithCoverage extends observe boost using structural edge novelty.
func CorpusObserveBoostWithCoverage(cfg map[string]any, recordFinding bool, newEdge, newPath bool, edgeBitmap []byte) int {
	return CorpusObserveBoostWithCoverageEx(cfg, recordFinding, false, newEdge, newPath, edgeBitmap)
}

// CorpusObserveBoostWithCoverageEx applies hang-aware observe boost (v2.9).
func CorpusObserveBoostWithCoverageEx(cfg map[string]any, recordFinding, hangOnly, newEdge, newPath bool, edgeBitmap []byte) int {
	boost := CorpusObserveBoostEx(recordFinding, hangOnly, newEdge, newPath)
	if !CoverageFeedbackEnabled(cfg) {
		return boost
	}
	if BitmapHasCoverageSignal(edgeBitmap) {
		n := BitmapEdgeCount(edgeBitmap)
		if n > 0 {
			boost += 1 + minInt(n/6, 6) // v2.8: denser bitmap → more energy
		}
	}
	if newEdge {
		boost += 1
	}
	if newEdge && newPath {
		boost += 3 // dual novelty is high-value AFL-style find
	}
	if recordFinding && hangOnly {
		boost += 1 // hang: smaller extras than crash
	} else if recordFinding {
		boost += 2
	}
	return boost
}

// StructuralRarityHint returns 0..3 from input shape entropy (cheap path novelty proxy).
func StructuralRarityHint(input []byte) int {
	if len(input) == 0 {
		return 0
	}
	score := StructuralFeatureScore(input)
	switch {
	case score >= 180:
		return 3
	case score >= 100:
		return 2
	case score >= 40:
		return 1
	default:
		return 0
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
