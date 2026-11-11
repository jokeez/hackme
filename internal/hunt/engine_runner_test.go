package hunt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIsLibFuzzerPreflightErr(t *testing.T) {
	if !isLibFuzzerPreflightErr(errors.New("hunt: libfuzzer import: clang required")) {
		t.Fatal("clang preflight")
	}
	if !isLibFuzzerPreflightErr(errors.New("clang subprocess libfuzzer: exit 1")) {
		t.Fatal("build preflight")
	}
	if !isLibFuzzerPreflightErr(errors.New(`fuzzupstream: unknown target "inv_fuzz_parse_c"`)) {
		t.Fatal("unknown inventory target must soft-skip (was 1ms CLEAN bug)")
	}
	if !isLibFuzzerPreflightErr(errors.New("hunt artifact: abc not found")) {
		t.Fatal("missing published harness is preflight")
	}
	if isLibFuzzerPreflightErr(errors.New("hunt: libfuzzer import: no corpus files in /tmp/x")) {
		t.Fatal("empty import after session must NOT soft-skip")
	}
	if isLibFuzzerPreflightErr(errors.New("hunt: libfuzzer persist: corpus still empty after 60s")) {
		t.Fatal("empty persist after wall must NOT soft-skip")
	}
	if isLibFuzzerPreflightErr(nil) {
		t.Fatal("nil")
	}
}

func TestSeedPersistentCorpusBootstrapsEmpty(t *testing.T) {
	dir := t.TempDir()
	corpus := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := seedPersistentCorpus(dir, "jsmn", corpus); err != nil {
		t.Fatal(err)
	}
	n, err := countCorpusFiles(corpus)
	if err != nil || n < 3 {
		t.Fatalf("want boot seeds, got n=%d err=%v", n, err)
	}
}

func TestRunLibFuzzerEngineShardEmptyImportStillRan(t *testing.T) {
	// Without a real target/manifest this hits preflight — ensure helper classification
	// matches the soft-skip contract used by runLibFuzzerEngineShard.
	err := errors.New("hunt: libfuzzer persist: no corpus files in /x (INFO: Running with entropic)")
	if isLibFuzzerPreflightErr(err) {
		t.Fatal("session empty corpus must remain Ran=true (CLEAN floor), not soft_skip")
	}
}

func TestIsLibFuzzerPreflightErrBwrap(t *testing.T) {
	if !isLibFuzzerPreflightErr(errors.New("hunt isolate: bwrap required (HACKME_HUNT_ISOLATE_STRICT)")) {
		t.Fatal("STRICT bwrap required is preflight soft-skip")
	}
	// Userns/RTM_NEWADDR is handled by bare retry — must NOT soft_skip the lease.
	if isLibFuzzerPreflightErr(errors.New("bwrap: Creating new namespace failed: Permission denied")) {
		t.Fatal("userns denial must not soft_skip")
	}
	if isLibFuzzerPreflightErr(errors.New("bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted")) {
		t.Fatal("RTM_NEWADDR must not soft_skip")
	}
}

func TestIsolateRequiredDefaultOn(t *testing.T) {
	t.Setenv("HACKME_HUNT_ISOLATE_REQUIRE", "")
	if !IsolateRequired() {
		t.Fatal("default ON")
	}
	t.Setenv("HACKME_HUNT_ISOLATE_REQUIRE", "0")
	if IsolateRequired() {
		t.Fatal("0 → off")
	}
}

func TestAFLEngineShardBindsHarnessDir(t *testing.T) {
	// Structural check: wrapBubblewrap path used by AFL must accept ExtraRo harness dir
	// without remounting the operator prefix (regression for H2 + C1).
	t.Setenv("HACKME_PREFIX", "/opt/hackme")
	tmp := t.TempDir()
	harnessDir := filepath.Join(tmp, "hunt-afl-bin")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(harnessDir, "target.bin")
	if err := os.WriteFile(harness, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(tmp, "work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	argv, ok := wrapBubblewrap("/bin/true", []string{"--", harness}, work,
		[]string{work}, []string{harnessDir, "/opt/hackme"})
	if !ok {
		t.Skip("bwrap not on PATH")
	}
	foundHarness := false
	for i := 0; i+2 < len(argv); i++ {
		if argv[i] == "--ro-bind-try" || argv[i] == "--ro-bind" {
			if filepath.Clean(argv[i+1]) == filepath.Clean(harnessDir) {
				foundHarness = true
			}
			if filepath.Clean(argv[i+1]) == "/opt/hackme" {
				t.Fatalf("prefix must not be RO-bound: %v", argv)
			}
		}
		if argv[i] == "--bind" && filepath.Clean(argv[i+1]) == "/opt/hackme" {
			t.Fatalf("prefix must not be RW-bound: %v", argv)
		}
	}
	if !foundHarness {
		t.Fatalf("expected harness dir ExtraRoBind in argv: %v", argv)
	}
}

func TestIsolateStrictDefaultOn(t *testing.T) {
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "")
	if !IsolateStrict() {
		t.Fatal("default ON")
	}
	t.Setenv("HACKME_HUNT_ISOLATE_STRICT", "0")
	if IsolateStrict() {
		t.Fatal("STRICT=0 must opt out")
	}
}

func TestBwrapCageFailedRTM(t *testing.T) {
	// Still classified for Start()-time decisions when STRICT is opted out.
	if !bwrapCageFailed(fmt.Errorf("exit status 1"), "bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted") {
		t.Fatal("RTM_NEWADDR must count as cage failure")
	}
}

func TestBwrapSetupFailedRequiresBwrapPrefix(t *testing.T) {
	if !bwrapSetupFailed("bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted") {
		t.Fatal("bwrap-prefixed RTM must count as setup failure")
	}
	if !bwrapSetupFailed("bwrap: setting up uid map: Permission denied") {
		t.Fatal("bwrap-prefixed uid_map must count as setup failure")
	}
	// Harness noise must not trigger unsandbox-after-Start.
	if bwrapSetupFailed("AddressSanitizer: permission denied") {
		t.Fatal("harness stderr must not count as bwrap setup failure")
	}
	if bwrapSetupFailed("operation not permitted") {
		t.Fatal("bare permission text without bwrap: must not unsandbox")
	}
	if !harnessLikelyRanInCage("bwrap: Permission denied\n==ERROR: AddressSanitizer: heap-buffer-overflow") {
		t.Fatal("ASAN after forged bwrap line must count as harness ran")
	}
	if harnessLikelyRanInCage("bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted") {
		t.Fatal("pure bwrap setup stderr must not look like harness ran")
	}
}

func TestIsLibFuzzerPreflightErrAFLWallShort(t *testing.T) {
	if !isLibFuzzerPreflightErr(errors.New("afl_wall_short:elapsed_ms=6000 want>=45000")) {
		t.Fatal("afl_wall_short must soft-skip (Deep was lying Ran=true)")
	}
}

func TestJoinEngineErr(t *testing.T) {
	if got := joinEngineErr("a", "", "c"); got != "a; c" {
		t.Fatalf("got %q", got)
	}
	if got := joinEngineErr("", "mid", ""); got != "mid" {
		t.Fatalf("got %q", got)
	}
}
