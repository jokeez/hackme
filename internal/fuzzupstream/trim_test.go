package fuzzupstream

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestSanitizerSame(t *testing.T) {
	a := SanitizerInfo{Class: "asan", Subtype: "heap-buffer-overflow", Security: true}
	b := SanitizerInfo{Class: "asan", Subtype: "heap-buffer-overflow", Security: true}
	if !SanitizerSame(a, b) {
		t.Fatal("want same")
	}
	c := SanitizerInfo{Class: "ubsan", Subtype: "shift-overflow", Security: false}
	if SanitizerSame(a, c) {
		t.Fatal("want different class")
	}
}

func TestTrimCrashInputClang(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang required")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "trim_harness.c")
	code := `#include <stdint.h>
#include <stddef.h>
int LLVMFuzzerTestOneInput(const uint8_t *d, size_t n) {
	if (n >= 12 && d[0]=='B' && d[1]=='O' && d[2]=='O' && d[3]=='M') {
		volatile int *p = 0;
		*p = 1;
	}
	return 0;
}`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "harness")
	cmd := exec.Command("clang", "-fsanitize=address", "-fno-omit-frame-pointer", "-g", "-O1", src, "-o", bin)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("clang build failed: %v %s", err, out)
	}
	input := append([]byte("PADPADPAD"), []byte("BOOMTRIGGER")...)
	opts := DefaultRunInputOpts()
	crash, want, _, err := RunInputDetailed(context.Background(), bin, input, opts)
	if err != nil || !crash {
		t.Fatalf("baseline crash missing err=%v crash=%v", err, crash)
	}
	tr := TrimCrashInput(context.Background(), bin, input, opts, want)
	if !tr.Trimmed || tr.TrimmedLen >= tr.OriginalLen {
		t.Fatalf("trim stats=%+v", tr)
	}
	if len(tr.Input) > 16 {
		t.Fatalf("still large trimmed=%q len=%d", tr.Input, len(tr.Input))
	}
	crash2, got, _, err := RunInputDetailed(context.Background(), bin, tr.Input, opts)
	if err != nil || !crash2 || !SanitizerSame(want, got) {
		t.Fatalf("trimmed repro failed err=%v crash=%v got=%+v", err, crash2, got)
	}
}

func TestReproCmdHuntNative(t *testing.T) {
	cmd := ReproCmdHuntNative([]byte("7b7d7d"))
	if cmd == "" || len(cmd) < 20 {
		t.Fatalf("cmd=%q", cmd)
	}
}
