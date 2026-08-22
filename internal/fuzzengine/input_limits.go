package fuzzengine

import "strings"

const (
	// DefaultMaxInputBytes is the prod WASM pool ceiling (matches sandbox InvokeCheckInput).
	DefaultMaxInputBytes = 4096
	// DefaultMaxInputBytesStd is the default customer tier preset.
	DefaultMaxInputBytesStd = 1024
	// MinMaxInputBytes is the smallest allowed byte corpus entry.
	MinMaxInputBytes = 8
	// MaxInputBytesHardCeil must stay aligned with sandbox.MaxCheckInputBytes().
	MaxInputBytesHardCeil = 4096
)

// ParseMaxInputBytes reads campaign max_input_bytes (clamped to platform ceil).
func ParseMaxInputBytes(cfg map[string]any) int {
	if cfg == nil {
		return DefaultMaxInputBytesStd
	}
	if v, ok := cfg["max_input_bytes"]; ok {
		n := intFromAny(v)
		return clampMaxInputBytes(n)
	}
	if ParseInputMode(cfg) == InputModeBytes {
		return DefaultMaxInputBytesStd
	}
	return 8
}

func clampMaxInputBytes(n int) int {
	if n < MinMaxInputBytes {
		return MinMaxInputBytes
	}
	if n > MaxInputBytesHardCeil {
		return MaxInputBytesHardCeil
	}
	return n
}

// ClampInputBytes truncates input to campaign max_input_bytes.
func ClampInputBytes(b []byte, cfg map[string]any) []byte {
	max := ParseMaxInputBytes(cfg)
	if len(b) <= max {
		return b
	}
	return append([]byte(nil), b[:max]...)
}

// ByteTierPreset returns recommended max_input_bytes for lab/prod presets.
func ByteTierPreset(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "lite", "256":
		return 256
	case "std", "1024", "1k":
		return 1024
	case "pro", "4096", "4k":
		return 4096
	default:
		return DefaultMaxInputBytesStd
	}
}
