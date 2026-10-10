package fuzzupstream

import (
	"strings"
	"testing"
)

func TestClassifySanitizerMSAN(t *testing.T) {
	blob := "==1==ERROR: MemorySanitizer: use-of-uninitialized-value\nSUMMARY: MemorySanitizer: use-of-uninitialized-value"
	info := ClassifySanitizer(blob)
	if info.Class != "msan" {
		t.Fatalf("class=%q", info.Class)
	}
	if info.Security {
		t.Fatal("MSAN must not be auto-security/CVE")
	}
	if !strings.Contains(SanitizerDisplayLabel(info), "MSAN") {
		t.Fatalf("label=%q", SanitizerDisplayLabel(info))
	}
}

func TestMSANLikelyFalsePositive(t *testing.T) {
	fp := "==ERROR: MemorySanitizer: use-of-uninitialized-value\n    #0 __interceptor_memcpy libc.so.6\n    #1 in libmsan"
	if !msanLikelyFalsePositive(fp, "/cache/oss-cve-clones/mpack", "mpack_deep_stdin") {
		t.Fatal("expected FP for libc-only stack")
	}
	stl := "==ERROR: MemorySanitizer: use-of-uninitialized-value\n    #0 in std::string\n    #1 /usr/lib/x86_64-linux-gnu/libstdc++.so.6"
	if !msanLikelyFalsePositive(stl, "/cache/oss-cve-clones/mpack", "mpack_deep_stdin") {
		t.Fatal("expected FP for libstdc++-only stack")
	}
	real := "==ERROR: MemorySanitizer: use-of-uninitialized-value\n    #0 in mpack_tree_parse\n    #1 /cache/oss-cve-clones/mpack/src/mpack/mpack-node.c"
	if msanLikelyFalsePositive(real, "/cache/oss-cve-clones/mpack", "mpack_deep_stdin") {
		t.Fatal("clone-framed hit must not be FP")
	}
	inproc := "==ERROR: MemorySanitizer: use-of-uninitialized-value\n    #0 in LLVMFuzzerTestOneInput\n    #1 tasks/sources/fuzz/benchmark/mpack_libfuzzer.c"
	if msanLikelyFalsePositive(inproc, "", "mpack_libfuzzer") {
		t.Fatal("in-process harness frame must not be FP")
	}
}
