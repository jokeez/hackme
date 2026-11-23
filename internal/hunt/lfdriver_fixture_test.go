package hunt

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"hackme/internal/fuzzengine"
)

// Real binaries from the tiny fixtures: an in-process LLVMFuzzerTestOneInput
// is a cache hit, a stripped stdin stub is not, and a symlink is not.
// The fuzzer then writes a crash artifact. Two heap sites pay twice, a slid
// offset pays once, and the leak input is not a paid crash.
func TestInProcessFixtureCacheCrashAndBounty(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang required")
	}
	dir := t.TempDir()
	inprocSrc := filepath.Join("testdata", "lfdriver", "inproc_fuzzer.c")
	stubSrc := filepath.Join("testdata", "lfdriver", "stdin_stub.c")
	prodStub := filepath.Join("..", "..", "tasks", "sources", "fuzz", "benchmark", "stdin_subprocess_libfuzzer.c")
	for _, p := range []string{inprocSrc, stubSrc, prodStub} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("fixture %s: %v", p, err)
		}
	}

	inproc := filepath.Join(dir, "inproc")
	// -O0: -O1 deletes the out-of-bounds stores as undefined behavior before
	// ASan can see them, and the binary then reports a clean run.
	compileFixture(t, clang, inproc, []string{
		"-fsanitize=fuzzer,address", "-fno-omit-frame-pointer", "-O0", "-g0",
		"-o", inproc, inprocSrc,
	})
	inprocRaw, err := os.ReadFile(inproc)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(inprocRaw, []byte("HACKME_LF_STDIN_BIN")) {
		t.Fatal("in-process fixture looks like the stdin stub")
	}
	if !cachedDedicatedLibFuzzerBin(inproc) {
		t.Fatal("in-process LLVMFuzzerTestOneInput must be a cache hit")
	}
	if kind := readLibFuzzerBinKind(inproc); kind != libFuzzerBinKindDedicated {
		t.Fatalf("cache kind=%q", kind)
	}
	if !cachedDedicatedLibFuzzerBin(inproc) {
		t.Fatal("second look must stay a cache hit")
	}
	link := filepath.Join(dir, "inproc-link")
	if err := os.Symlink(inproc, link); err != nil {
		t.Fatal(err)
	}
	if cachedDedicatedLibFuzzerBin(link) {
		t.Fatal("symlink must not be a cache hit")
	}
	if _, err := os.Stat(link + ".kind"); err == nil {
		t.Fatal("symlink cache check wrote a kind sidecar")
	}

	stub := filepath.Join(dir, "stdin-stub")
	compileFixture(t, clang, stub, []string{
		"-fsanitize=fuzzer,address", "-fno-omit-frame-pointer", "-O1", "-g0", "-Wl,-s",
		"-o", stub, prodStub,
	})
	if cachedDedicatedLibFuzzerBin(stub) {
		t.Fatal("production stdin stub must not be a cache hit")
	}
	if kind := readLibFuzzerBinKind(stub); kind != libFuzzerBinKindStub {
		t.Fatalf("stub kind=%q", kind)
	}
	if cachedDedicatedLibFuzzerBin(stub) {
		t.Fatal("sidecar must keep the stdin stub rejected")
	}

	// The production filename is also stored in sanitizer metadata, so a scan
	// for that path rejects the real driver. This stub's only marker is the
	// getenv key in .rodata. A filename scan would cache it as in-process.
	tiny := filepath.Join(dir, "tiny-stub")
	compileFixture(t, clang, tiny, []string{
		"-fsanitize=fuzzer,address", "-fno-omit-frame-pointer", "-O1", "-g0", "-Wl,-s",
		"-o", tiny, stubSrc,
	})
	tinyRaw, err := os.ReadFile(tiny)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(tinyRaw, []byte("stdin_subprocess_libfuzzer.c")) {
		t.Fatal("tiny stub unexpectedly embeds the production filename")
	}
	if !bytes.Contains(tinyRaw, []byte("HACKME_LF_STDIN_BIN")) {
		t.Fatal("tiny stub lost HACKME_LF_STDIN_BIN")
	}
	if cachedDedicatedLibFuzzerBin(tiny) {
		t.Fatal("stdin stub without the filename must not be a cache hit")
	}

	rawA := runFuzzerSeed(t, inproc, []byte("A-bug"), false)
	rawB := runFuzzerSeed(t, inproc, []byte("B-bug"), false)
	for _, raw := range []string{rawA, rawB} {
		if !strings.Contains(raw, "ERROR: AddressSanitizer: heap-buffer-overflow") {
			t.Fatalf("not a heap-buffer-overflow from the fixture:\n%s", raw)
		}
	}
	keyA := fuzzengine.StableCrashBucket("native_crash", rawA)
	keyB := fuzzengine.StableCrashBucket("native_crash", rawB)
	if keyA == keyB || !strings.HasPrefix(keyA, "crash|heap_oob|") || !strings.HasPrefix(keyB, "crash|heap_oob|") {
		t.Fatalf("two fixture bugs must pay twice: a=%q b=%q", keyA, keyB)
	}
	slidA := slideFixtureOffsets(rawA, 0x1000)
	if slidA == rawA {
		t.Fatal("slide did not move a target offset")
	}
	if got := fuzzengine.StableCrashBucket("native_crash", slidA); got != keyA {
		t.Fatalf("offset slide minted a second bounty:\n got %q\nwant %q", got, keyA)
	}
	if got := fuzzengine.StableCrashBucket("native_crash", slideFixtureOffsets(rawB, 0x1000)); got != keyB {
		t.Fatalf("offset slide minted a second bounty for B:\n got %q\nwant %q", got, keyB)
	}

	runFuzzerSeed(t, inproc, []byte("L-leak"), true)

	leakBin := filepath.Join(dir, "leak")
	compileFixture(t, clang, leakBin, []string{
		"-fsanitize=address", "-fno-omit-frame-pointer", "-g", "-O0", "-DLF_DRIVER_MAIN",
		"-o", leakBin, inprocSrc,
	})
	leakReport := runLeakBin(t, leakBin)
	if !strings.Contains(leakReport, "LeakSanitizer") && !strings.Contains(leakReport, "leaked") {
		t.Fatalf("not a real leak report:\n%s", leakReport)
	}
	leakKey := fuzzengine.StableCrashBucket("native_crash", leakReport)
	if strings.HasPrefix(leakKey, "crash|") || !strings.HasPrefix(leakKey, "hygiene|lsan") {
		t.Fatalf("leak looked like a paid crash: %q", leakKey)
	}
}

func compileFixture(t *testing.T, clang, out string, args []string) {
	t.Helper()
	cmd := exec.Command(clang, args...)
	cmd.Env = sanitizerEnv(nil)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("compile %s: %v\n%s", out, err, raw)
	}
}

// runFuzzerSeed runs one input. wantNoCrash fails the test when libFuzzer writes
// a crash-* artifact.
func runFuzzerSeed(t *testing.T, bin string, seed []byte, wantNoCrash bool) string {
	t.Helper()
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(corpus, "seed"), seed, 0o644); err != nil {
		t.Fatal(err)
	}
	art := filepath.Join(dir, "art")
	if err := os.MkdirAll(art, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin,
		"-runs=1", "-timeout=5",
		"-artifact_prefix="+art+string(filepath.Separator)+"crash-",
		corpus,
	)
	cmd.Env = sanitizerEnv([]string{"ASAN_OPTIONS=detect_leaks=0:abort_on_error=1:halt_on_error=1:symbolize=0:allocator_may_return_null=1"})
	out, _ := cmd.CombinedOutput()
	ents, err := os.ReadDir(art)
	if err != nil {
		t.Fatal(err)
	}
	var crashBody []byte
	for _, e := range ents {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "crash-") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(art, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		crashBody = b
	}
	if wantNoCrash {
		if crashBody != nil {
			t.Fatalf("leak seed wrote %q", crashBody)
		}
		return string(out)
	}
	if crashBody == nil {
		t.Fatalf("no crash artifact for %q\n%s", seed, out)
	}
	if !bytes.Equal(crashBody, seed) {
		t.Fatalf("crash artifact=%q want %q", crashBody, seed)
	}
	return string(out)
}

func runLeakBin(t *testing.T, bin string) string {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Env = sanitizerEnv([]string{"ASAN_OPTIONS=detect_leaks=1:halt_on_error=0:symbolize=0"})
	out, _ := cmd.CombinedOutput()
	return string(out)
}

func sanitizerEnv(extra []string) []string {
	env := make([]string, 0, len(os.Environ())+len(extra))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "ASAN_OPTIONS=") || strings.HasPrefix(e, "UBSAN_OPTIONS=") || strings.HasPrefix(e, "LSAN_OPTIONS=") {
			continue
		}
		env = append(env, e)
	}
	return append(env, extra...)
}

var reFixtureModOff = regexp.MustCompile(`\(([^()\s]+)\+(0x[0-9a-fA-F]+)\)`)

func slideFixtureOffsets(report string, delta uint64) string {
	return reFixtureModOff.ReplaceAllStringFunc(report, func(m string) string {
		sub := reFixtureModOff.FindStringSubmatch(m)
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
