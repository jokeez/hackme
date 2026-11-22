package fuzzengine

import "testing"

const liveASanFixture = `=================================================================
==572474==ERROR: AddressSanitizer: stack-buffer-overflow on address 0x7afb73300020
SUMMARY: AddressSanitizer: stack-buffer-overflow /usr/include/x86_64-linux-gnu/bits/string_fortified.h:59 in memset
`

func TestStableCrashBucketLiveASan(t *testing.T) {
	key := StableCrashBucket("asan", liveASanFixture)
	if key != "crash|stack_oob|memset" {
		t.Fatalf("got %q want crash|stack_oob|memset", key)
	}
	a := StableCrashBucket("asan", liveASanFixture)
	b := StableCrashBucket("asan", "==99999==ERROR: AddressSanitizer: stack-buffer-overflow\nSUMMARY: … in memset")
	if a != b {
		t.Fatalf("same site should dedup: %q vs %q", a, b)
	}
	fortify := StableCrashBucket("native_crash", "*** buffer overflow detected ***\n#0 in memset")
	if fortify != "crash|stack_oob|memset" {
		t.Fatalf("fortify merge: %q", fortify)
	}
}

func TestStableCrashBucketSymbolize0PC(t *testing.T) {
	rawA := `==1==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x602000000014 at pc 0x0000555555551234 bp 0x7fff sp 0x7fff
READ of size 1 at 0x602000000014 thread T0
    #0 0x555555551233 (/tmp/hunt-a/harness+0x4c3a11)
SUMMARY: AddressSanitizer: heap-buffer-overflow (/tmp/hunt-a/harness+0x4c3a11)
`
	rawB := `==99==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x60200000ffff at pc 0x00007fffdeadbeef bp 0x1 sp 0x1
READ of size 1 at 0x60200000ffff thread T0
    #0 0x7fffdeadbeee (/tmp/hunt-b/harness+0x00004c3a11)
SUMMARY: AddressSanitizer: heap-buffer-overflow (/tmp/hunt-b/harness+0x00004c3a11)
`
	rawOther := `==2==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x1 at pc 0x2 bp 0x3 sp 0x4
    #0 0x2 (/tmp/hunt-a/harness+0x51aa02)
SUMMARY: AddressSanitizer: heap-buffer-overflow (/tmp/hunt-a/harness+0x51aa02)
`
	trapA := "hunt_crash:heap-buffer-overflow pc=+0x4c3a11"
	keyA := StableCrashBucket("native_crash", rawA)
	keyB := StableCrashBucket("native_crash", rawB)
	keyOther := StableCrashBucket("native_crash", rawOther)
	keyTrap := StableCrashBucket("native_crash", trapA)
	if keyA != "crash|heap_oob|+0x4c3a11" || keyA != keyB || keyA != keyTrap {
		t.Fatalf("same bug collapsed wrong: a=%q b=%q trap=%q", keyA, keyB, keyTrap)
	}
	if keyOther == keyA || keyOther != "crash|heap_oob|+0x51aa02" {
		t.Fatalf("different PC must pay separately: %q", keyOther)
	}
	// Interceptor frame is not the bug. The target offset is.
	memcpy := `==3==ERROR: AddressSanitizer: heap-buffer-overflow on address 0x10 at pc 0x20 bp 0x30 sp 0x40
    #0 0x7f (/usr/lib/llvm-18/lib/clang/18/lib/linux/libclang_rt.asan-x86_64.so+0x9c1a0)
    #1 0x55 (/tmp/hunt-a/harness+0x4c3a11)
SUMMARY: AddressSanitizer: heap-buffer-overflow (/usr/lib/llvm-18/lib/clang/18/lib/linux/libclang_rt.asan-x86_64.so+0x9abcde) in __interceptor_memcpy
`
	if got := StableCrashBucket("native_crash", memcpy); got != keyA {
		t.Fatalf("runtime frame must not hide the target PC: %q", got)
	}
	// No module offset: symbolized site stays the bucket (legacy fixture).
	if got := StableCrashBucket("asan", liveASanFixture); got != "crash|stack_oob|memset" {
		t.Fatalf("symbolized site %q", got)
	}
	// Two stack overflows without a PC still share the legacy site. With a PC they do not.
	stackOther := `==4==ERROR: AddressSanitizer: stack-buffer-overflow on address 0x1
SUMMARY: AddressSanitizer: stack-buffer-overflow (/tmp/hunt-a/harness+0x1111)
`
	if got := StableCrashBucket("asan", stackOther); got != "crash|stack_oob|+0x1111" {
		t.Fatalf("stack pc %q", got)
	}
	if got := StableCrashBucket("native_crash", "hunt_crash:heap-buffer-overflow"); got == keyA {
		t.Fatal("legacy subtype-only trap must not equal a PC bucket")
	}
}

func TestStableFindingKeyFromDetail(t *testing.T) {
	k := StableFindingKeyFromDetail("asan", map[string]any{
		"trap": liveASanFixture,
	})
	if k != "crash|stack_oob|memset" {
		t.Fatalf("got %q", k)
	}
}
