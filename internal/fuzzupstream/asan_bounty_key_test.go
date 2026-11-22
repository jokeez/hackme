package fuzzupstream

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"hackme/internal/fuzzengine"
)

// Two real AddressSanitizer runs: same subtype at different sites pays twice,
// the same site pays once, and sliding every target offset (a rebuild that
// only moves instructions) does not mint a second bounty.
func TestRealASANBountyKey(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang required for a real ASAN report")
	}
	dir := t.TempDir()
	writeSrc(t, filepath.Join(dir, "a.c"), `
#include <stdlib.h>
__attribute__((noinline)) void leaf_a(char *p) { p[24] = 'A'; }
__attribute__((noinline)) void mid_a(char *p) { leaf_a(p); }
int main(void) {
  char *p = (char *)malloc(8);
  mid_a(p);
  return 0;
}
`)
	writeSrc(t, filepath.Join(dir, "b.c"), `
#include <stdlib.h>
#include <string.h>
__attribute__((noinline)) void leaf_b(char *p) { memcpy(p, "0123456789abcdef0123456789abcdefXXXX", 36); }
__attribute__((noinline)) void mid_b(char *p) { leaf_b(p); }
__attribute__((noinline)) void outer_b(char *p) { mid_b(p); }
int main(void) {
  char *p = (char *)malloc(4);
  outer_b(p);
  return 0;
}
`)
	binA := compileSan(t, clang, dir, "a", "address")
	binB := compileSan(t, clang, dir, "b", "address")
	opt0 := []string{"ASAN_OPTIONS=abort_on_error=1:detect_leaks=0:symbolize=0:print_stacktrace=1"}
	rawA := runSan(t, binA, opt0)
	rawA2 := runSan(t, binA, opt0)
	rawB := runSan(t, binB, opt0)
	for _, raw := range []string{rawA, rawA2, rawB} {
		if !strings.Contains(raw, "ERROR: AddressSanitizer: heap-buffer-overflow") {
			t.Fatalf("not a real heap-buffer-overflow report:\n%s", raw)
		}
	}
	keyA := fuzzengine.StableCrashBucket("native_crash", rawA)
	keyA2 := fuzzengine.StableCrashBucket("native_crash", rawA2)
	keyB := fuzzengine.StableCrashBucket("native_crash", rawB)
	if keyA != keyA2 {
		t.Fatalf("same site paid twice: %q vs %q", keyA, keyA2)
	}
	if keyA == keyB || !strings.HasPrefix(keyA, "crash|heap_oob|") || !strings.HasPrefix(keyB, "crash|heap_oob|") {
		t.Fatalf("different bugs must pay twice: a=%q b=%q", keyA, keyB)
	}
	slidA := slideTargetOffsets(rawA, 0x1000)
	if slidA == rawA {
		t.Fatal("slide did not move a target offset")
	}
	if got := fuzzengine.StableCrashBucket("native_crash", slidA); got != keyA {
		t.Fatalf("offset-only slide minted a second bounty:\n got %q\nwant %q", got, keyA)
	}
	if trapKey := fuzzengine.StableCrashBucket("native_crash", FormatHuntTrap(ClassifySanitizer(rawA))); trapKey != keyA {
		t.Fatalf("stamped trap %q != raw %q", trapKey, keyA)
	}
	if trapKey := fuzzengine.StableCrashBucket("native_crash", FormatHuntTrap(ClassifySanitizer(slidA))); trapKey != keyA {
		t.Fatalf("slid trap %q != original %q", trapKey, keyA)
	}
	if trapKey := fuzzengine.StableCrashBucket("native_crash", FormatHuntTrap(ClassifySanitizer(rawB))); trapKey != keyB {
		t.Fatalf("bug B trap %q != raw %q", trapKey, keyB)
	}

	opt1 := []string{"ASAN_OPTIONS=abort_on_error=1:detect_leaks=0:symbolize=1:print_stacktrace=1"}
	sym := runSan(t, binA, opt1)
	if strings.Contains(sym, "in leaf_a") {
		symKey := fuzzengine.StableCrashBucket("native_crash", sym)
		if !strings.Contains(symKey, "leaf_a") {
			t.Fatalf("symbolized report ignored the function: %q", symKey)
		}
		if got := fuzzengine.StableCrashBucket("native_crash", slideTargetOffsets(sym, 0x2222)); got != symKey {
			t.Fatalf("symbolized slide changed the bounty: %q vs %q", got, symKey)
		}
		if fuzzengine.StableCrashBucket("native_crash", FormatHuntTrap(ClassifySanitizer(sym))) != symKey {
			t.Fatal("symbolized trap diverged from the raw report")
		}
	}
}

func TestRealUBSanAndLeakAreNotPaidCrashes(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang required")
	}
	dir := t.TempDir()
	writeSrc(t, filepath.Join(dir, "ub.c"), `
int main(void) {
  int x = 1073741824;
  return x * 2;
}
`)
	writeSrc(t, filepath.Join(dir, "leak.c"), `
#include <stdlib.h>
int main(void) {
  char *p = (char *)malloc(32);
  p[0] = 'L';
  return p[0] == 'L' ? 0 : 1;
}
`)
	ub := runSan(t, compileSan(t, clang, dir, "ub", "undefined"), []string{"UBSAN_OPTIONS=print_stacktrace=1:halt_on_error=1"})
	if !strings.Contains(ub, "runtime error:") {
		t.Fatalf("not a real UBSan report:\n%s", ub)
	}
	ubKey := fuzzengine.StableCrashBucket("native_crash", ub)
	if strings.HasPrefix(ubKey, "crash|") || !strings.HasPrefix(ubKey, "hygiene|ubsan") {
		t.Fatalf("UBSan looked like a paid crash: %q", ubKey)
	}
	if info := ClassifySanitizer(ub); info.Security || info.Class != "ubsan" {
		t.Fatalf("UBSan classified as a paying crash: %+v", info)
	}

	leak := runSan(t, compileSan(t, clang, dir, "leak", "address"), []string{"ASAN_OPTIONS=detect_leaks=1:halt_on_error=0:symbolize=0"})
	if !strings.Contains(leak, "LeakSanitizer") && !strings.Contains(leak, "leaked") {
		t.Fatalf("not a real leak report:\n%s", leak)
	}
	leakKey := fuzzengine.StableCrashBucket("native_crash", leak)
	if strings.HasPrefix(leakKey, "crash|") || !strings.HasPrefix(leakKey, "hygiene|lsan") {
		t.Fatalf("leak looked like a paid crash: %q", leakKey)
	}
	if info := ClassifySanitizer(leak); info.Security {
		t.Fatalf("leak classified as security: %+v", info)
	}
}

func writeSrc(t *testing.T, path, src string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

func compileSan(t *testing.T, clang, dir, name, sanitizer string) string {
	t.Helper()
	bin := filepath.Join(dir, name)
	cmd := exec.Command(clang, "-fsanitize="+sanitizer, "-fno-omit-frame-pointer", "-g", "-O0", "-o", bin, filepath.Join(dir, name+".c"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile %s: %v\n%s", name, err, out)
	}
	return bin
}

func runSan(t *testing.T, bin string, extra []string) string {
	t.Helper()
	cmd := exec.Command(bin)
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "ASAN_OPTIONS=") || strings.HasPrefix(e, "UBSAN_OPTIONS=") || strings.HasPrefix(e, "LSAN_OPTIONS=") {
			continue
		}
		env = append(env, e)
	}
	cmd.Env = append(env, extra...)
	out, _ := cmd.CombinedOutput()
	return string(out)
}

var reModOff = regexp.MustCompile(`\(([^()\s]+)\+(0x[0-9a-fA-F]+)\)`)

func slideTargetOffsets(report string, delta uint64) string {
	return reModOff.ReplaceAllStringFunc(report, func(m string) string {
		sub := reModOff.FindStringSubmatch(m)
		if len(sub) != 3 {
			return m
		}
		low := strings.ToLower(sub[1])
		if strings.Contains(low, "libc") || strings.Contains(low, "ld-linux") ||
			strings.Contains(low, "libasan") || strings.Contains(low, "libclang_rt") {
			return m
		}
		n, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(sub[2]), "0x"), 16, 64)
		if err != nil {
			return m
		}
		return "(" + sub[1] + "+0x" + strconv.FormatUint(n+delta, 16) + ")"
	})
}
