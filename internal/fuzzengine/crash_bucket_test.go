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

func TestStableFindingKeyFromDetail(t *testing.T) {
	k := StableFindingKeyFromDetail("asan", map[string]any{
		"trap": liveASanFixture,
	})
	if k != "crash|stack_oob|memset" {
		t.Fatalf("got %q", k)
	}
}
