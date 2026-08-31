package fuzzupstream

import "testing"

func TestClassifySanitizerASAN(t *testing.T) {
	blob := `==1==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x602000000010
SUMMARY: AddressSanitizer: heap-buffer-overflow in parse_json`
	info := ClassifySanitizer(blob)
	if info.Class != "asan" || info.Subtype != "heap-buffer-overflow" || !info.Security {
		t.Fatalf("info=%+v", info)
	}
	if info.Label != "ASAN · heap-buffer-overflow" {
		t.Fatalf("label=%q", info.Label)
	}
}

func TestClassifySanitizerUBSanShift(t *testing.T) {
	blob := `runtime error: signed integer overflow: 1073741824 * 2 cannot be represented in type 'int'
SUMMARY: UndefinedBehaviorSanitizer: undefined-behavior`
	info := ClassifySanitizer(blob)
	if info.Class != "ubsan" || info.Subtype != "signed-overflow" || info.Security {
		t.Fatalf("info=%+v", info)
	}
	if info.Label != "UBSan · signed-overflow" {
		t.Fatalf("label=%q", info.Label)
	}
}

func TestClassifySanitizerUBSanNullDeref(t *testing.T) {
	blob := `runtime error: member call on address 0x000000000000 which does not point to an object of type 'Parser'
SUMMARY: UndefinedBehaviorSanitizer: undefined-behavior`
	info := ClassifySanitizer(blob)
	if info.Subtype != "null-deref" {
		t.Fatalf("info=%+v", info)
	}
}

func TestClassifySanitizerUBSanFnPointer(t *testing.T) {
	blob := `runtime error: call to function through pointer to incorrect function type
SUMMARY: UndefinedBehaviorSanitizer: undefined-behavior`
	info := ClassifySanitizer(blob)
	if info.Subtype != "function-pointer-cast" {
		t.Fatalf("info=%+v", info)
	}
}

func TestClassifySanitizerLSan(t *testing.T) {
	blob := `
SUMMARY: LeakSanitizer: 32 byte(s) leaked in 1 allocation(s).
Direct leak of 32 byte(s) in 1 object(s) allocated from:
`
	info := ClassifySanitizer(blob)
	if info.Class != "lsan" || info.Subtype != "direct-leak" || info.Security {
		t.Fatalf("info=%+v", info)
	}
}

func TestFormatAndParseHuntTrap(t *testing.T) {
	sec := FormatHuntTrap(SanitizerInfo{Class: "asan", Subtype: "heap-buffer-overflow", Security: true})
	if sec != "hunt_crash:heap-buffer-overflow" {
		t.Fatalf("sec trap=%q", sec)
	}
	info := SanitizerInfo{Class: "ubsan", Subtype: "shift-overflow", Security: false}
	trap := FormatHuntTrap(info)
	if trap != "hunt_sanitizer:ubsan/shift-overflow" {
		t.Fatalf("trap=%q", trap)
	}
	got, ok := ParseHuntTrap(trap)
	if !ok || got.Class != "ubsan" || got.Subtype != "shift-overflow" || got.Security {
		t.Fatalf("parse=%+v ok=%v", got, ok)
	}
}

func TestDetectLeaksEnabledDefault(t *testing.T) {
	t.Setenv("HACKME_HUNT_DETECT_LEAKS", "")
	if !DetectLeaksEnabled() {
		t.Fatal("expected default leaks on")
	}
	t.Setenv("HACKME_HUNT_DETECT_LEAKS", "0")
	if DetectLeaksEnabled() {
		t.Fatal("expected leaks off when env=0")
	}
}

func TestAsanOptionsDetectLeaks(t *testing.T) {
	if !containsAll(asanOptions(true), "detect_leaks=1") {
		t.Fatal(asanOptions(true))
	}
	if containsAll(asanOptions(false), "detect_leaks=1") {
		t.Fatal(asanOptions(false))
	}
}

func containsAll(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
