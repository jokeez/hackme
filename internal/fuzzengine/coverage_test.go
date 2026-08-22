package fuzzengine

import "testing"

func TestCoverageBucketsForExecWasmEdge(t *testing.T) {
	cfg := map[string]any{"coverage_kind": CoverageKindWasmEdgeBitmap}
	bitmap := make([]byte, 256)
	bitmap[42] = 3
	edge, path := CoverageBucketsForExec(cfg, 0, []byte("AKIA"), bitmap)
	if edge == 0 || path == 0 {
		t.Fatalf("expected non-zero buckets from bitmap edge=%d path=%d", edge, path)
	}
	edge2, path2 := CoverageBucketsForExec(cfg, 0, []byte("AKIA"), bitmap)
	if edge != edge2 || path != path2 {
		t.Fatalf("bitmap buckets not deterministic")
	}
}

func TestCoverageBucketsForExecFingerprintFallback(t *testing.T) {
	cfg := map[string]any{"coverage_kind": CoverageKindWasmEdgeBitmap}
	edge, path := CoverageBucketsForExec(cfg, 12345, nil, nil)
	e2, p2 := CoverageBuckets(12345)
	if edge != e2 || path != p2 {
		t.Fatalf("empty bitmap should fall back to fingerprint")
	}
}
