package hunt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/hunt"
)

// The catalog's stdin drivers are the fuzz targets for ~60 ids that ship no dedicated
// harness. Running them in-process instead of fork+exec per input is worth two orders
// of magnitude (measured on zlib: 92 -> 17,627 exec/s, 7 -> 263 edges, and those 7
// were the wrapper's own edges, not the target's), so the eligibility rule has to be
// exact: the shim calls main(void) once per input and a driver that calls exit() would
// end the session instead of the input.
func TestInprocStdinDriverSupportedRules(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ok := write("good.c", "#include <stdio.h>\nint main(void) { char b[8]; fread(b,1,8,stdin); return 0; }\n")
	if !hunt.InprocStdinDriverSupported(ok) {
		t.Error("a main(void) driver without exit() must be supported")
	}
	for name, body := range map[string]string{
		"argv.c":    "int main(int argc, char **argv) { (void)argc; (void)argv; return 0; }\n",
		"exit.c":    "#include <stdlib.h>\nint main(void) { exit(0); }\n",
		"underexit": "#include <unistd.h>\nint main(void) { _exit(0); }\n",
	} {
		if hunt.InprocStdinDriverSupported(write(name+".c", body)) {
			t.Errorf("%s must be refused", name)
		}
	}
	for _, bad := range []string{"", "   ", filepath.Join(dir, "missing.c"), filepath.Join(dir, "driver.rs")} {
		if hunt.InprocStdinDriverSupported(bad) {
			t.Errorf("%q must be refused", bad)
		}
	}
	// A word ending in "exit" is not a call site.
	if !hunt.InprocStdinDriverSupported(write("name.c", "int on_exit_handler;\nint main(void) { return on_exit_handler; }\n")) {
		t.Error("an identifier containing exit must not disqualify a driver")
	}
}

// Every driver the catalog ships today qualifies. If a new one does not, Hunt silently
// drops that target to 92 exec/s of blind fuzzing — this test makes that a decision
// rather than an accident.
func TestCatalogStdinDriversRunInProcess(t *testing.T) {
	root := repoRootForHuntTest(t)
	dir := filepath.Join(root, "tasks", "sources", "fuzz", "oss")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("driver dir unavailable: %v", err)
	}
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_stdin.c") {
			continue
		}
		checked++
		p := filepath.Join(dir, e.Name())
		if !hunt.InprocStdinDriverSupported(p) {
			raw, _ := os.ReadFile(p)
			t.Errorf("%s cannot run in-process (needs main(void) and no exit()):\n%s", e.Name(), head(string(raw), 400))
		}
	}
	if checked == 0 {
		t.Skip("no stdin drivers on disk")
	}
}

func head(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// The shim is the thing that makes the rail work, so compile it for real and prove
// three properties: it links against an untouched driver, it needs no stdin binary in
// the environment, and a crash in the target still reaches the sanitizer.
func TestInprocShimRunsTargetAndReportsCrashes(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	root := repoRootForHuntTest(t)
	shim := filepath.Join(root, "tasks", "sources", "fuzz", "benchmark", "stdin_inproc_libfuzzer.c")
	selftest := filepath.Join(root, "tasks", "sources", "fuzz", "selftest")
	for _, p := range []string{shim, filepath.Join(selftest, "asanlab.c"), filepath.Join(selftest, "asanlab_stdin.c")} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("missing %s", p)
		}
	}
	if !hunt.InprocStdinDriverSupported(filepath.Join(selftest, "asanlab_stdin.c")) {
		t.Fatal("the selftest driver should qualify for the in-process rail")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "lf")
	build := exec.Command("clang", "-g", "-O1", "-fsanitize=address,fuzzer", "-fno-omit-frame-pointer",
		"-Dmain=hackme_stdin_main", "-I", selftest,
		shim, filepath.Join(selftest, "asanlab.c"), filepath.Join(selftest, "asanlab_stdin.c"),
		"-o", bin)
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("in-process build unavailable: %v: %s", err, out)
	}

	corpus := filepath.Join(dir, "corpus")
	if err := os.MkdirAll(corpus, 0o755); err != nil {
		t.Fatal(err)
	}
	// A use-after-free trigger, handed in as a seed: the run must reproduce it.
	if err := os.WriteFile(filepath.Join(corpus, "uaf.bin"), []byte{'A', 'L', 0x13, 0x55}, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, corpus, "-max_total_time=10", "-max_len=64", "-print_final_stats=1",
		"-detect_leaks=0", "-artifact_prefix="+dir+"/")
	// Deliberately no HACKME_LF_STDIN_BIN: the in-process rail must not need it.
	cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0:abort_on_error=0")
	out, _ := cmd.CombinedOutput()
	got := string(out)
	if !strings.Contains(got, "heap-use-after-free") {
		t.Fatalf("the target's crash did not reach the sanitizer:\n%s", head(got, 1200))
	}
	if strings.Contains(got, "HACKME_LF_STDIN_BIN") {
		t.Errorf("the in-process rail must not reference the fork wrapper's env:\n%s", head(got, 600))
	}
}

// The gate that kept the whole pool on the fork wrapper: the plan asked "is there a
// hand-written harness?" (7 of 68 ids) instead of "can this target run in-process?".
// Every catalog id with a usable stdin driver must now take the in-process rail.
func TestDriverPlanPrefersInProcessForEveryCatalogTarget(t *testing.T) {
	root := repoRootForHuntTest(t)
	dir := filepath.Join(root, "tasks", "sources", "fuzz", "oss")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("driver dir unavailable: %v", err)
	}
	checked, inproc := 0, 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "_stdin.c") {
			continue
		}
		id := strings.TrimSuffix(e.Name(), "_stdin.c")
		if !hunt.InprocStdinDriverSupported(filepath.Join(dir, e.Name())) {
			continue
		}
		checked++
		if hunt.CatalogTargetRunsInProcess(root, id) {
			inproc++
		} else {
			t.Errorf("%s has a usable stdin driver but would still fork+exec per input", id)
		}
	}
	if checked == 0 {
		t.Skip("no usable drivers on disk")
	}
	if inproc != checked {
		t.Fatalf("%d of %d catalog targets take the in-process rail", inproc, checked)
	}
	t.Logf("in-process rail covers %d catalog targets", inproc)
}
