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

// ClassifyFinding returns triage metadata for report HTML/JSON.
func ClassifyFinding(findingType, severity string) Triage {
	ft := strings.TrimSpace(strings.ToLower(findingType))
	sev := strings.TrimSpace(strings.ToLower(severity))
	switch ft {
	case "crash":
		return Triage{
			Class:       "needs_triage",
			Label:       "Crash / trap",
			Note:        "Sandbox reported a trap or abort. Reproduce with repro_cmd; confirm in native upstream only before claiming 0-day.",
			ZeroDayHint: "unknown_until_native_repro",
		}
	case "sandbox_reject":
		return Triage{
			Class:       "sandbox",
			Label:       "Sandbox reject",
			Note:        "Module failed validation or was quarantined. Usually not a vulnerability in your guard logic.",
			ZeroDayHint: "low",
		}
	case "property_violation":
		return Triage{
			Class:       "expected_signal",
			Label:       "Property signal",
			Note:        "check() returned outside the accepting set (detector/PoW semantics). Often expected fuzz noise, not exploitable crash.",
			ZeroDayHint: "low",
		}
	default:
		_ = sev
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
