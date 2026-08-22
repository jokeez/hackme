package fuzzengine

import (
	"hash/fnv"
	"strings"
)

const (
	CoverageKindInputFingerprint = "input_fingerprint"
	CoverageKindWasmEdgeBitmap   = "wasm_edge_bitmap"
)

// CoverageUsesWasmEdge returns true when config requests wasm edge bitmap scheduling.
func CoverageUsesWasmEdge(cfg map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(CoverageKind(cfg)), CoverageKindWasmEdgeBitmap)
}

// BitmapHasCoverageSignal returns true when sandbox returned a non-empty edge bitmap.
func BitmapHasCoverageSignal(bitmap []byte) bool {
	for _, c := range bitmap {
		if c != 0 {
			return true
		}
	}
	return false
}

// CoverageBucketsForExec returns edge/path scheduling buckets for one exec.
// When coverage_kind=wasm_edge_bitmap and bitmap has signal, buckets derive from WASM edges.
// Otherwise falls back to input fingerprint (honest hybrid until guard is instrumented).
func CoverageBucketsForExec(cfg map[string]any, inputU uint64, inputB []byte, edgeBitmap []byte) (edgeBucket, pathBucket int) {
	if CoverageUsesWasmEdge(cfg) && BitmapHasCoverageSignal(edgeBitmap) {
		return primaryEdgeFromBitmap(edgeBitmap), pathBucketFromBitmap(edgeBitmap)
	}
	if len(inputB) > 0 {
		return CoverageBucketsFromBytes(inputB)
	}
	return CoverageBuckets(inputU)
}

func primaryEdgeFromBitmap(bitmap []byte) int {
	if len(bitmap) == 0 {
		return 0
	}
	bestIdx, bestVal := 0, 0
	for i, c := range bitmap {
		if int(c) > bestVal {
			bestVal = int(c)
			bestIdx = i
		}
	}
	if bestVal == 0 {
		return 0
	}
	return (bestIdx*17 + bestVal) % 257
}

func pathBucketFromBitmap(bitmap []byte) int {
	if len(bitmap) == 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write(bitmap)
	return int(h.Sum64() % 509)
}
