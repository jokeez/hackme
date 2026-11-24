package fuzzupstream

import (
	"context"
	"strings"
	"testing"
	"time"
)

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

func TestClassifySanitizerIntegratedASANLeakNotCVE(t *testing.T) {
	// Clang often prints LSan under the AddressSanitizer SUMMARY banner.
	blob := `
    #0 0x5 in ParseField xjson.c:567
    #1 0x6 in ParseObject xjson.c:621
SUMMARY: AddressSanitizer: 144 byte(s) leaked in 2 allocation(s).
`
	info := ClassifySanitizer(blob)
	if info.Class != "lsan" || info.Security {
		t.Fatalf("integrated ASAN leak must be lsan/non-security, got %+v", info)
	}
	if IsSecuritySanitizer(blob) {
		t.Fatal("IsSecuritySanitizer must reject ASAN leak summary")
	}
}

func TestFormatAndParseHuntTrap(t *testing.T) {
	sec := FormatHuntTrap(SanitizerInfo{Class: "asan", Subtype: "heap-buffer-overflow", Security: true})
	if sec != "hunt_crash:heap-buffer-overflow" {
		t.Fatalf("sec trap=%q", sec)
	}
	withPC := FormatHuntTrap(SanitizerInfo{Class: "asan", Subtype: "heap-buffer-overflow", Security: true, PC: "+0x00004C3A11"})
	if withPC != "hunt_crash:heap-buffer-overflow pc=+0x4c3a11" {
		t.Fatalf("pc trap=%q", withPC)
	}
	parsed, ok := ParseHuntTrap(withPC)
	if !ok || !parsed.Security || parsed.Subtype != "heap-buffer-overflow" || parsed.PC != "+0x4c3a11" {
		t.Fatalf("parse pc=%+v ok=%v", parsed, ok)
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
	for _, leaks := range []bool{false, true} {
		opt := asanOptions(leaks)
		if !containsAll(opt, "symbolize=0") || !containsAll(opt, "abort_on_error=1") {
			t.Fatal(opt)
		}
	}
}

func TestClassifyHarnessRunTimeoutKeepsSecurityASAN(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	blob := "==1==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x1\nSUMMARY: AddressSanitizer: heap-buffer-overflow (/tmp/h+0x4c3a11)\n"
	crash, info, _, err := classifyHarnessRun(ctx, ctx.Err(), blob)
	if err != nil || !crash || !info.Security || info.PC != "+0x4c3a11" {
		t.Fatalf("crash=%v info=%+v err=%v", crash, info, err)
	}
	if !strings.HasPrefix(FormatHuntTrap(info), "hunt_crash:heap-buffer-overflow pc=") {
		t.Fatalf("trap=%q", FormatHuntTrap(info))
	}
	// Unsigned overflow is informational. A hang must not become a paying crash.
	ub := "runtime error: unsigned integer overflow: 1 + 1 cannot be represented in type 'unsigned int'\n"
	crash, _, _, err = classifyHarnessRun(ctx, ctx.Err(), ub)
	if crash || err == nil {
		t.Fatalf("ubsan timeout crash=%v err=%v", crash, err)
	}
	crash, _, _, err = classifyHarnessRun(ctx, ctx.Err(), "still running\n")
	if crash || err == nil {
		t.Fatalf("bare timeout crash=%v err=%v", crash, err)
	}
}

func TestClassifySanitizerSymbolize0PC(t *testing.T) {
	blob := `==1==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x602000000014 at pc 0x0000555555551234
    #0 0x7f (/usr/lib/llvm-18/lib/clang/18/lib/linux/libclang_rt.asan-x86_64.so+0x9c1a0)
    #1 0x55 (/tmp/hunt-a/harness+0x4c3a11)
SUMMARY: AddressSanitizer: heap-buffer-overflow (/usr/lib/llvm-18/lib/clang/18/lib/linux/libclang_rt.asan-x86_64.so+0x9c1a0)
`
	info := ClassifySanitizer(blob)
	if !info.Security || info.Subtype != "heap-buffer-overflow" || info.PC != "+0x4c3a11" {
		t.Fatalf("info=%+v", info)
	}
	if FormatHuntTrap(info) != "hunt_crash:heap-buffer-overflow pc=+0x4c3a11" {
		t.Fatalf("trap=%q", FormatHuntTrap(info))
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
