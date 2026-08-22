package sandbox

import (
	"hash/fnv"
)

// Linear-memory coverage contract for instrumented check_bytes guards (Phase 2 P1).
const (
	CovBitmapMemOff = 8192
	CovBitmapLen    = 256
)

// CheckOutcome is the result of one sandbox check invocation.
type CheckOutcome struct {
	OK         bool
	EdgeBitmap []byte // copied from wasm memory when present; may be nil
}

// ReadCovBitmap copies the coverage region from wasm linear memory (best-effort).
func ReadCovBitmap(mem interface {
	Read(offset uint32, byteCount uint32) ([]byte, bool)
}) []byte {
	if mem == nil {
		return nil
	}
	defer func() { _ = recover() }()
	b, ok := mem.Read(CovBitmapMemOff, CovBitmapLen)
	if !ok || len(b) == 0 {
		return nil
	}
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// BitmapHasSignal returns true when any coverage byte is non-zero.
func BitmapHasSignal(bitmap []byte) bool {
	for _, c := range bitmap {
		if c != 0 {
			return true
		}
	}
	return false
}

// PrimaryEdgeFromBitmap picks a stable scheduling bucket from a wasm edge bitmap.
func PrimaryEdgeFromBitmap(bitmap []byte) int {
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

// PathBucketFromBitmap hashes the full bitmap for path scheduling diversity.
func PathBucketFromBitmap(bitmap []byte) int {
	if len(bitmap) == 0 {
		return 0
	}
	h := fnv.New64a()
	_, _ = h.Write(bitmap)
	return int(h.Sum64() % 509)
}
