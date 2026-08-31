package fuzzupstream

import (
	"fmt"
	"os"
	"strings"
)

// RunInputOpts configures one ASAN/UBSan/LSan stdin harness execution.
type RunInputOpts struct {
	MaxInput    int
	DetectLeaks bool
}

// SanitizerInfo is a classified sanitizer signal from harness stderr.
type SanitizerInfo struct {
	Class    string `json:"sanitizer_class"`   // asan, ubsan, lsan
	Subtype  string `json:"sanitizer_subtype"` // heap-buffer-overflow, shift-overflow, ...
	Raw      string `json:"sanitizer_raw,omitempty"`
	Label    string `json:"sanitizer_label,omitempty"`
	Security bool   `json:"security"`
}

// DefaultRunInputOpts returns global Hunt stdin exec defaults.
func DefaultRunInputOpts() RunInputOpts {
	return defaultRunInputOpts(65536)
}

// DetectLeaksEnabled is the default LSan toggle (env HACKME_HUNT_DETECT_LEAKS).
func DetectLeaksEnabled() bool {
	return DetectLeaksDefault()
}

// RunInputOptsFromConfig reads max_input_bytes and hunt_detect_leaks from campaign config.
func RunInputOptsFromConfig(cfg map[string]any) RunInputOpts {
	opts := DefaultRunInputOpts()
	if cfg == nil {
		return opts
	}
	if v, ok := cfg["max_input_bytes"]; ok {
		if n := intFromConfig(v); n > 0 {
			opts.MaxInput = n
		}
	}
	if v, ok := cfg["hunt_detect_leaks"]; ok {
		opts.DetectLeaks = truthyConfig(v)
	}
	return opts
}

// ClassifySanitizer parses harness output and fills display label.
func ClassifySanitizer(blob string) SanitizerInfo {
	info, ok := ClassifySanitizerOutput(blob)
	if !ok {
		return SanitizerInfo{}
	}
	info.Label = SanitizerDisplayLabel(info)
	return info
}

// SanitizerDisplayLabel renders customer-facing class · subtype text.
func SanitizerDisplayLabel(info SanitizerInfo) string {
	if info.Class == "" {
		return ""
	}
	sub := info.Subtype
	if sub == "" {
		sub = "unknown"
	}
	return sanitizerClassDisplay(info.Class) + " · " + sub
}

func sanitizerClassDisplay(class string) string {
	switch strings.ToLower(strings.TrimSpace(class)) {
	case "asan":
		return "ASAN"
	case "ubsan":
		return "UBSan"
	case "lsan":
		return "LSan"
	default:
		return strings.ToUpper(class)
	}
}

func intFromConfig(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	case string:
		var n int
		_, _ = fmt.Sscanf(strings.TrimSpace(x), "%d", &n)
		return n
	default:
		return 0
	}
}

func truthyConfig(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.TrimSpace(strings.ToLower(x))
		return s == "1" || s == "true" || s == "yes" || s == "on"
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	default:
		return false
	}
}

// DetectLeaksDefault reads HACKME_HUNT_DETECT_LEAKS (default on).
func DetectLeaksDefault() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("HACKME_HUNT_DETECT_LEAKS")))
	switch v {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func defaultRunInputOpts(maxInput int) RunInputOpts {
	if maxInput <= 0 {
		maxInput = 65536
	}
	return RunInputOpts{
		MaxInput:    maxInput,
		DetectLeaks: DetectLeaksDefault(),
	}
}

func asanOptions(detectLeaks bool) string {
	leak := "0"
	if detectLeaks {
		leak = "1"
	}
	return "detect_leaks=" + leak + ":halt_on_error=1:allocator_may_return_null=1:print_stacktrace=1"
}

// ClassifySanitizerOutput parses full harness output into class/subtype/tier.
func ClassifySanitizerOutput(blob string) (info SanitizerInfo, ok bool) {
	blob = strings.TrimSpace(blob)
	if blob == "" {
		return SanitizerInfo{}, false
	}
	low := strings.ToLower(blob)

	// ASAN memory safety first (bounty lane).
	for _, pair := range []struct {
		sig, subtype string
	}{
		{"heap-buffer-overflow", "heap-buffer-overflow"},
		{"stack-buffer-overflow", "stack-buffer-overflow"},
		{"heap-use-after-free", "use-after-free"},
		{"use-after-free", "use-after-free"},
		{"double-free", "double-free"},
		{"attempting double-free", "double-free"},
		{"segv on unknown address", "segv-unknown"},
	} {
		if strings.Contains(low, pair.sig) {
			return SanitizerInfo{
				Class: "asan", Subtype: pair.subtype, Raw: pair.sig, Security: true,
			}, true
		}
	}
	if strings.Contains(low, "summary: addresssanitizer") {
		sub := "address-sanitizer"
		if strings.Contains(low, "stack") {
			sub = "stack-buffer-overflow"
		} else if strings.Contains(low, "heap") {
			sub = "heap-buffer-overflow"
		}
		return SanitizerInfo{Class: "asan", Subtype: sub, Raw: "SUMMARY: AddressSanitizer", Security: true}, true
	}

	// LeakSanitizer (informational hygiene).
	if strings.Contains(low, "summary: leaksanitizer") ||
		strings.Contains(low, "detected memory leaks") ||
		strings.Contains(low, "direct leak") ||
		strings.Contains(low, "indirect leak") {
		sub := "memory-leak"
		switch {
		case strings.Contains(low, "direct leak"):
			sub = "direct-leak"
		case strings.Contains(low, "indirect leak"):
			sub = "indirect-leak"
		}
		return SanitizerInfo{Class: "lsan", Subtype: sub, Raw: "SUMMARY: LeakSanitizer", Security: false}, true
	}

	// UBSan / runtime UB (informational).
	if strings.Contains(low, "runtime error:") || strings.Contains(low, "undefinedbehaviorsanitizer") {
		sub := classifyUBSanSubtype(low)
		raw := "runtime error:"
		if strings.Contains(low, "summary: undefinedbehaviorsanitizer") {
			raw = "SUMMARY: UndefinedBehaviorSanitizer"
		}
		return SanitizerInfo{Class: "ubsan", Subtype: sub, Raw: raw, Security: false}, true
	}
	return SanitizerInfo{}, false
}

func classifyUBSanSubtype(low string) string {
	switch {
	case strings.Contains(low, "shift exponent") || strings.Contains(low, "shift base"):
		return "shift-overflow"
	case strings.Contains(low, "signed integer overflow"):
		return "signed-overflow"
	case strings.Contains(low, "unsigned integer overflow"):
		return "unsigned-overflow"
	case strings.Contains(low, "integer overflow"):
		return "integer-overflow"
	case strings.Contains(low, "division by zero") || strings.Contains(low, "divide by zero"):
		return "div-by-zero"
	case strings.Contains(low, "member call on address") || strings.Contains(low, "member access within"):
		return "null-deref"
	case strings.Contains(low, "null pointer"):
		return "null-deref"
	case strings.Contains(low, "function pointer") || strings.Contains(low, "function type") ||
		strings.Contains(low, "incompatible function"):
		return "function-pointer-cast"
	case strings.Contains(low, "misaligned address"):
		return "misaligned-pointer"
	case strings.Contains(low, "load of value") && strings.Contains(low, "not a valid"):
		return "invalid-bool-load"
	case strings.Contains(low, "not a valid value for enumeration"):
		return "invalid-enum"
	case strings.Contains(low, "dynamic type mismatch"):
		return "vptr-type-mismatch"
	case strings.Contains(low, "non-positive vla"):
		return "invalid-vla"
	case strings.Contains(low, "alignment"):
		return "alignment-violation"
	default:
		return "undefined-behavior"
	}
}

// IsSecuritySanitizer reports bounty-eligible ASAN-class signals.
func IsSecuritySanitizer(san string) bool {
	info, ok := ClassifySanitizerOutput(san)
	if ok {
		return info.Security
	}
	// Legacy raw token from detectSanitizer.
	if strings.Contains(san, "UndefinedBehaviorSanitizer") || strings.Contains(san, "runtime error:") {
		return false
	}
	if strings.Contains(san, "LeakSanitizer") || strings.Contains(san, "leak") {
		return false
	}
	return strings.Contains(san, "AddressSanitizer") ||
		strings.Contains(san, "heap-buffer-overflow") ||
		strings.Contains(san, "stack-buffer-overflow") ||
		strings.Contains(san, "use-after-free") ||
		strings.Contains(san, "double-free") ||
		strings.Contains(san, "SEGV on unknown address")
}

// IsInformationalSanitizer reports UBSan/LSan hygiene signals (finding, not bounty).
func IsInformationalSanitizer(san string) bool {
	info, ok := ClassifySanitizerOutput(san)
	if ok {
		return !info.Security
	}
	return strings.Contains(san, "UndefinedBehaviorSanitizer") ||
		strings.Contains(san, "runtime error:") ||
		strings.Contains(san, "LeakSanitizer")
}

// HuntTrap encodes sanitizer class/subtype for pool worker ↔ coordinator exchange.
func HuntTrap(info SanitizerInfo) string {
	if info.Class == "" {
		return "hunt_crash:unknown"
	}
	if info.Security {
		sub := info.Subtype
		if sub == "" {
			sub = "address-sanitizer"
		}
		return "hunt_crash:" + sub
	}
	sub := info.Subtype
	if sub == "" {
		sub = "undefined-behavior"
	}
	return "hunt_sanitizer:" + info.Class + "/" + sub
}

// FormatHuntTrap is an alias for HuntTrap (pool trap wire format).
func FormatHuntTrap(info SanitizerInfo) string {
	return HuntTrap(info)
}

// ParseHuntTrap decodes hunt_crash / hunt_sanitizer trap strings.
func ParseHuntTrap(trap string) (SanitizerInfo, bool) {
	trap = strings.TrimSpace(trap)
	switch {
	case strings.HasPrefix(trap, "hunt_sanitizer:"):
		rest := strings.TrimPrefix(trap, "hunt_sanitizer:")
		parts := strings.SplitN(rest, "/", 2)
		info := SanitizerInfo{Class: parts[0], Security: false}
		if len(parts) == 2 {
			info.Subtype = parts[1]
		}
		info.Label = SanitizerDisplayLabel(info)
		return info, info.Class != ""
	case strings.HasPrefix(trap, "hunt_crash:"):
		sub := strings.TrimPrefix(trap, "hunt_crash:")
		info := SanitizerInfo{Class: "asan", Subtype: sub, Security: true}
		info.Label = SanitizerDisplayLabel(info)
		return info, true
	default:
		return SanitizerInfo{}, false
	}
}

// InformationalSeverity maps hygiene subtypes to report severity (never bounty).
func InformationalSeverity(info SanitizerInfo) string {
	switch info.Class {
	case "lsan":
		return "low"
	case "ubsan":
		switch info.Subtype {
		case "null-deref", "function-pointer-cast", "misaligned-pointer":
			return "low"
		case "shift-overflow", "signed-overflow", "integer-overflow", "div-by-zero":
			return "info"
		default:
			return "info"
		}
	default:
		return "info"
	}
}

// InformationalTitle builds a customer-facing finding title.
func InformationalTitle(info SanitizerInfo, target string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		target = "target"
	}
	label := sanitizerClassDisplay(info.Class) + " " + strings.ReplaceAll(info.Subtype, "-", " ")
	return "Hunt " + label + " on " + target
}
