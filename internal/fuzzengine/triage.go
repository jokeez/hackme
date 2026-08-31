package fuzzengine

import (
	"fmt"
	"strings"
)

// Triage describes how developers should interpret a finding (not a CVE claim).
type Triage struct {
	Class       string `json:"class"`
	Label       string `json:"label"`
	Note        string `json:"note"`
	ZeroDayHint string `json:"zero_day_hint"`
}

// IsCrashClass reports whether a finding belongs in the customer top triage
// (crash / hang / ASan / memory) rather than detector coverage noise.
func IsCrashClass(findingType string) bool {
	ft := strings.TrimSpace(strings.ToLower(findingType))
	if ft == "" {
		return false
	}
	switch ft {
	case "crash", "hang", "timeout", "timeout_hang", "asan", "ubsan", "msan", "tsan",
		"memory", "memory_error", "leak", "oom", "segfault", "sigsegv", "abort",
		"native_crash", "heap_overflow", "stack_overflow", "use_after_free":
		return true
	}
	for _, needle := range []string{
		"crash", "hang", "asan", "ubsan", "msan", "tsan", "segfault",
		"sigsegv", "oom", "leak", "memory", "timeout", "abort",
	} {
		if strings.Contains(ft, needle) {
			return true
		}
	}
	return false
}

// IsCoverageNoise reports detector / property / guard signals that should stay
// in the report appendix, not the crash-first top issues list.
func IsCoverageNoise(findingType string) bool {
	if IsCrashClass(findingType) {
		return false
	}
	ft := strings.TrimSpace(strings.ToLower(findingType))
	switch ft {
	case "property_violation", "security_violation", "consensus_script_push",
		"interesting_input", "sandbox_reject", "harness_runtime":
		return true
	}
	for _, needle := range []string{"property", "detector", "guard", "violation", "signal"} {
		if strings.Contains(ft, needle) {
			return true
		}
	}
	return false
}

// ClassifyFinding returns triage metadata for report HTML/JSON.
func ClassifyFinding(findingType, severity string) Triage {
	ft := strings.TrimSpace(strings.ToLower(findingType))
	sev := strings.TrimSpace(strings.ToLower(severity))
	_ = sev
	if IsCrashClass(ft) {
		label := "Crash / trap"
		note := "Sandbox or harness reported a crash-class signal. Reproduce with repro_cmd; confirm in native upstream only before claiming 0-day."
		switch {
		case strings.Contains(ft, "hang") || strings.Contains(ft, "timeout"):
			label = "Hang / timeout"
			note = "Execution exceeded time budget or hung. Reproduce locally; confirm against native target before severity claims."
		case strings.Contains(ft, "asan") || strings.Contains(ft, "ubsan") || strings.Contains(ft, "msan") ||
			strings.Contains(ft, "tsan") || strings.Contains(ft, "memory") || strings.Contains(ft, "leak") ||
			strings.Contains(ft, "oom") || strings.Contains(ft, "overflow") || strings.Contains(ft, "use_after"):
			label = "Memory / sanitizer"
			note = "Sanitizer or memory-safety signal. Reproduce with the same input; treat as crash-class until cleared."
		case strings.Contains(ft, "native"):
			label = "Native crash"
			note = "Native/upstream repro confirmed a crash. Highest priority for customer triage."
		}
		return Triage{
			Class:       "needs_triage",
			Label:       label,
			Note:        note,
			ZeroDayHint: "unknown_until_native_repro",
		}
	}
	switch ft {
	case "sandbox_reject", "harness_runtime":
		return Triage{
			Class:       "harness_noise",
			Label:       "Harness / runtime",
			Note:        "Sandbox cache, validation, or WASM runtime failure — not a bug in the fuzz target. Fix the harness/runtime before treating as a finding.",
			ZeroDayHint: "low",
		}
	case "property_violation":
		return Triage{
			Class:       "expected_signal",
			Label:       "Coverage noise",
			Note:        "check() returned outside the accepting set (detector/PoW semantics). Appendix-only: not a crash-class issue.",
			ZeroDayHint: "low",
		}
	case "consensus_script_push", "security_violation", "interesting_input":
		return Triage{
			Class:       "coverage_noise",
			Label:       "Coverage noise",
			Note:        "Detector/guard signal. Listed under coverage noise — not a crash/hang/ASan finding.",
			ZeroDayHint: "low",
		}
	case "sanitizer_informational":
		return Triage{
			Class:       "sanitizer_hygiene",
			Label:       "Sanitizer hygiene",
			Note:        "UBSan/LSan informational signal — quality/triage lane, not bounty-eligible. Fix before release on hardened builds.",
			ZeroDayHint: "low",
		}
	default:
		if IsCoverageNoise(ft) {
			return Triage{
				Class:       "coverage_noise",
				Label:       "Coverage noise",
				Note:        "Non-crash detector/property signal. Appendix-only for customer reports.",
				ZeroDayHint: "low",
			}
		}
		return Triage{
			Class:       "review",
			Label:       "Review",
			Note:        "Manual review recommended.",
			ZeroDayHint: "unknown",
		}
	}
}

// ReproCmdTool returns a copy-paste local repro command when wasm path is known.
func ReproCmdTool(wasmPath string, input uint64) string {
	if strings.TrimSpace(wasmPath) == "" {
		return ReproCmd(input)
	}
	return fmt.Sprintf("go run ./tools/check_repro -wasm %q -input %q", wasmPath, fmt.Sprintf("0x%x", input))
}
