package hunt

import (
	"strings"

	"hackme/internal/fuzzupstream"
)

// ApplySanitizerDefaults sets Hunt sanitizer profile (ASAN+UBSan+LSan) on campaign config.
func ApplySanitizerDefaults(cfg map[string]any, pkgKey string) {
	if cfg == nil {
		return
	}
	if _, ok := cfg["hunt_detect_leaks"]; !ok {
		cfg["hunt_detect_leaks"] = true
	}
	if _, ok := cfg["hunt_sanitizer_profile"]; !ok {
		profile := "asan+ubsan+lsan"
		if !DetectLeaksFromConfig(cfg) {
			profile = "asan+ubsan"
		}
		cfg["hunt_sanitizer_profile"] = profile
	}
	if _, ok := cfg["hunt_trim"]; !ok {
		cfg["hunt_trim"] = true
	}
	_ = pkgKey
}

// DetectLeaksFromConfig reads hunt_detect_leaks from campaign config.
func DetectLeaksFromConfig(cfg map[string]any) bool {
	if cfg == nil {
		return fuzzupstream.DetectLeaksEnabled()
	}
	if _, ok := cfg["hunt_detect_leaks"]; ok {
		return fuzzupstream.RunInputOptsFromConfig(cfg).DetectLeaks
	}
	return fuzzupstream.DetectLeaksEnabled()
}

// RunInputOptsFromConfig returns sanitizer execution options for one Hunt campaign.
func RunInputOptsFromConfig(cfg map[string]any) fuzzupstream.RunInputOpts {
	return fuzzupstream.RunInputOptsFromConfig(cfg)
}

// SanitizerProfileLabel returns a short profile label for reports.
func SanitizerProfileLabel(cfg map[string]any) string {
	if cfg != nil {
		if v := strings.TrimSpace(strings.ToLower(stringFromAny(cfg["hunt_sanitizer_profile"]))); v != "" {
			return v
		}
	}
	if DetectLeaksFromConfig(cfg) {
		return "asan+ubsan+lsan"
	}
	return "asan+ubsan"
}

func stringFromAny(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// HuntTrimEnabled reports whether crash inputs are minimized before findings/reports.
func HuntTrimEnabled(cfg map[string]any) bool {
	if cfg == nil {
		return true
	}
	if v, ok := cfg["hunt_trim"]; ok {
		return cfgTruthy(v)
	}
	return true
}
