package fuzzupstream

import (
	"context"
)

const defaultMaxTrimSteps = 384

// TrimResult holds minimized crash input metadata.
type TrimResult struct {
	Input        []byte
	OriginalLen  int
	TrimmedLen   int
	Steps        int
	Trimmed      bool
}

// SanitizerSame reports whether two sanitizer classifications are equivalent for trim.
func SanitizerSame(a, b SanitizerInfo) bool {
	if a.Class != b.Class {
		return false
	}
	if a.Subtype != "" && b.Subtype != "" {
		return a.Subtype == b.Subtype
	}
	if a.Security != b.Security {
		return false
	}
	return true
}

// TrimCrashInput shrinks input while preserving the same sanitizer class/subtype (afl-tmin style).
func TrimCrashInput(ctx context.Context, binPath string, input []byte, opts RunInputOpts, want SanitizerInfo) TrimResult {
	res := TrimResult{
		Input:       append([]byte(nil), input...),
		OriginalLen: len(input),
		TrimmedLen:  len(input),
	}
	if len(input) <= 1 || binPath == "" {
		return res
	}
	if !crashMatches(ctx, binPath, input, opts, want) {
		return res
	}
	cur := append([]byte(nil), input...)
	steps := 0
	maxSteps := defaultMaxTrimSteps

	// Chunk removal (powers of two).
	for step := len(cur) / 2; step > 0 && steps < maxSteps; step /= 2 {
		if step == 0 {
			break
		}
		for start := 0; start+step <= len(cur) && steps < maxSteps; {
			trial := append([]byte(nil), cur[:start]...)
			trial = append(trial, cur[start+step:]...)
			if len(trial) == 0 {
				start += step
				continue
			}
			steps++
			if crashMatches(ctx, binPath, trial, opts, want) {
				cur = trial
			} else {
				start += step
			}
		}
	}

	// Per-byte removal from end toward front.
	for len(cur) > 1 && steps < maxSteps {
		removed := false
		for i := len(cur) - 1; i >= 0 && steps < maxSteps; i-- {
			trial := append([]byte(nil), cur[:i]...)
			trial = append(trial, cur[i+1:]...)
			if len(trial) == 0 {
				continue
			}
			steps++
			if crashMatches(ctx, binPath, trial, opts, want) {
				cur = trial
				removed = true
				break
			}
		}
		if !removed {
			break
		}
	}

	res.Input = cur
	res.TrimmedLen = len(cur)
	res.Steps = steps
	res.Trimmed = len(cur) < res.OriginalLen
	return res
}

func crashMatches(ctx context.Context, binPath string, input []byte, opts RunInputOpts, want SanitizerInfo) bool {
	crash, info, _, err := RunInputDetailed(ctx, binPath, input, opts)
	if err != nil || !crash {
		return false
	}
	return SanitizerSame(want, info)
}
