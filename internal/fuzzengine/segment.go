package fuzzengine

import (
	"context"
	"strings"
)

const (
	defaultExecPerUnitScan  = 1
	defaultExecPerUnitAudit = 64
	defaultExecPerUnitDeep  = 512
	maxExecPerUnitHardCeil  = 20000
)

// CoverageKind describes how corpus scheduling buckets inputs (honest product copy).
func CoverageKind(cfg map[string]any) string {
	if cfg != nil {
		if s := strings.TrimSpace(toString(cfg["coverage_kind"])); s != "" {
			return s
		}
	}
	return "input_fingerprint"
}

// ExecPerUnit returns deterministic exec count per pool work item (segment size).
func ExecPerUnit(cfg map[string]any) int {
	if cfg != nil {
		if v, ok := cfg["exec_per_unit"]; ok {
			return clampExecPerUnit(intFromAny(v))
		}
	}
	switch ParseDepthTier(cfg) {
	case DepthBytesCorpus, DepthUpstreamBinary, DepthOSSCVE:
		return defaultExecPerUnitDeep
	case DepthWasmNative:
		return defaultExecPerUnitAudit
	default:
		return defaultExecPerUnitScan
	}
}

func clampExecPerUnit(n int) int {
	if n < 1 {
		return 1
	}
	if n > maxExecPerUnitHardCeil {
		return maxExecPerUnitHardCeil
	}
	return n
}

func segmentSalt(inputN, execIdx uint64) uint64 {
	return inputN ^ (execIdx * 0x9E3779B97F4A7C15)
}

func byteAnchorBase(inputN uint64, cfg map[string]any) []byte {
	seeds := ParseByteCorpus(cfg)
	if len(seeds) > 0 {
		return append([]byte(nil), seeds[inputN%uint64(len(seeds))]...)
	}
	var buf [8]byte
	u := DeriveInput(inputN, cfg)
	for i := 0; i < 8; i++ {
		buf[i] = byte(u >> (8 * i))
	}
	return buf[:]
}

func spliceBytes(a, b []byte, maxLen int) []byte {
	if maxLen <= 0 {
		maxLen = DefaultMaxInputBytesStd
	}
	if len(a) == 0 {
		return append([]byte(nil), b...)
	}
	if len(b) == 0 {
		return append([]byte(nil), a...)
	}
	mid := len(a) / 2
	if mid == 0 {
		mid = 1
	}
	out := append(append([]byte(nil), a[:mid]...), b...)
	if len(out) > maxLen {
		out = out[:maxLen]
	}
	return out
}

// SegmentExecInput derives deterministic input for exec idx within a work unit.
// execIdx 0 is the anchor input (claim/submit anti-cheat).
func SegmentExecInput(inputN, execIdx uint64, cfg map[string]any, seeds []PoolCorpusSeed) (uint64, []byte) {
	if execIdx == 0 && GuidedSchedulingEnabled(cfg) && len(seeds) > 0 {
		return GuidedInputForWork(inputN, cfg, seeds)
	}
	maxLen := ParseMaxInputBytes(cfg)
	cap := PowerMutCap(cfg)
	if ExecPerUnit(cfg) > 1 && cap < 8 {
		cap = 8
	}
	stageCount := StageDeterministicMax + cap
	stage := MutationStage(int((inputN + execIdx*131) % uint64(stageCount)))
	salt := segmentSalt(inputN, execIdx)

	if ParseInputMode(cfg) == InputModeBytes {
		var base []byte
		if execIdx == 0 && len(seeds) > 0 && GuidedSchedulingEnabled(cfg) {
			seed := PickWeightedSeed(seeds, inputN)
			base = seed.InputBytes
			if len(base) == 0 {
				base = U64LayoutToBytes(seed.Input)
			}
		} else if execIdx > 0 && len(seeds) >= 2 && execIdx%19 == 0 {
			a := PickWeightedSeed(seeds, inputN)
			b := PickWeightedSeed(seeds, inputN+execIdx)
			ab, bb := a.InputBytes, b.InputBytes
			if len(ab) == 0 {
				ab = U64LayoutToBytes(a.Input)
			}
			if len(bb) == 0 {
				bb = U64LayoutToBytes(b.Input)
			}
			base = spliceBytes(ab, bb, maxLen)
		} else {
			base = byteAnchorBase(inputN, cfg)
		}
		b := MutateBytes(base, stage, salt, maxLen)
		if execIdx == 0 {
			b = ClampInputBytes(b, cfg)
		}
		return PackInputBytesToU64(b), b
	}

	var base uint64
	if len(seeds) > 0 && GuidedSchedulingEnabled(cfg) {
		seed := PickWeightedSeed(seeds, inputN)
		base = seed.Input
	} else {
		base = DeriveInput(inputN, cfg)
	}
	if execIdx == 0 {
		return base, nil
	}
	return MutateInput(base, stage, salt), nil
}

// SegmentResult is the outcome of replaying a full work segment on the coordinator.
type SegmentResult struct {
	ExecDone       int
	ExecExpected   int
	CheckResult    int32
	Trap           string
	Pass           bool
	RecordFinding  bool
	FindingInputU  uint64
	FindingInputB  []byte
	UniqueEdgeSeen int
	ExecCoverage   []CoverageSample
}

// CoverageSample is one exec's scheduling buckets (edge/path).
type CoverageSample struct {
	Edge int
	Path int
}

type segmentRunner func(ctx context.Context, inputU uint64, inputB []byte) (checkResult int32, trap string, execErr error, edgeBitmap []byte)

// EvalSegment replays exec_per_unit inputs deterministically (coordinator verify path).
func EvalSegment(ctx context.Context, inputN uint64, cfg map[string]any, seeds []PoolCorpusSeed, sem CheckSemantics, run segmentRunner) SegmentResult {
	expected := ExecPerUnit(cfg)
	out := SegmentResult{ExecExpected: expected}
	edgeSeen := map[int]struct{}{}
	var findingU uint64
	var findingB []byte
	var findingSet bool

	for execIdx := uint64(0); execIdx < uint64(expected); execIdx++ {
		if err := ctx.Err(); err != nil {
			out.Trap = err.Error()
			break
		}
		inU, inB := SegmentExecInput(inputN, execIdx, cfg, seeds)
		checkRet, trap, execErr, edgeBitmap := run(ctx, inU, inB)
		edge, path := CoverageBucketsForExec(cfg, inU, inB, edgeBitmap)
		edgeSeen[edge] = struct{}{}
		out.ExecCoverage = append(out.ExecCoverage, CoverageSample{Edge: edge, Path: path})
		out.ExecDone++
		pass, record := EvalCheck(sem, checkRet, execErr)
		if execErr != nil {
			trap = execErr.Error()
		}
		if record && !findingSet {
			findingSet = true
			findingU, findingB = inU, append([]byte(nil), inB...)
			out.CheckResult = checkRet
			out.Trap = trap
			out.Pass = pass
			out.RecordFinding = true
		}
		if trap != "" && out.Trap == "" {
			out.Trap = trap
		}
	}
	out.UniqueEdgeSeen = len(edgeSeen)
	if !findingSet {
		out.Pass = true
		out.RecordFinding = false
		if out.CheckResult == 0 {
			out.CheckResult = 1
		}
	}
	if findingSet {
		out.FindingInputU = findingU
		out.FindingInputB = findingB
	}
	return out
}
