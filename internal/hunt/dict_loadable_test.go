package hunt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hackme/internal/hunt"
)

// libFuzzer's ParseDictionaryFile only accepts comments and quoted values. A single
// bare line aborts the whole dictionary and the process exits before it fuzzes, so a
// malformed profile dict costs the entire lease: 0 execs, 0 coverage, a CLEAN on the
// floor. Every id the fleet can hand to a worker must produce a loadable dict.
var dictProfileIDs = []string{
	"cjson", "jansson", "frozen", "expat", "libxml2", "inih", "libucl",
	"mpack", "cmp", "tinycbor", "duktape", "quickjs", "zlib", "miniz", "lz4",
	"pcre2", "oniguruma", "heatshrink", "minimp3", "stb_image", "picohttpparser",
	"hiredis", "inv_customer_pack", "customer_repo_unknown_id", "",
}

func TestLibFuzzerProfileDictsAreLoadableGrammar(t *testing.T) {
	for _, id := range dictProfileIDs {
		text := hunt.LibFuzzerDictTextForTarget(id)
		if strings.TrimSpace(text) == "" {
			continue // no dict for this id is fine; a malformed one is not
		}
		for n, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if !strings.HasPrefix(line, `"`) || !strings.HasSuffix(line, `"`) {
				t.Errorf("target %q line %d is not quoted and would abort the dictionary: %q", id, n+1, line)
			}
		}
	}
}

// The grammar check above encodes what libFuzzer accepts; this one asks libFuzzer.
func TestLibFuzzerAcceptsGeneratedDicts(t *testing.T) {
	if _, err := exec.LookPath("clang"); err != nil {
		t.Skip("clang not installed")
	}
	root := repoRootForHuntTest(t)
	src := filepath.Join(root, "tasks", "sources", "fuzz", "selftest")
	if _, err := os.Stat(filepath.Join(src, "asanlab.c")); err != nil {
		t.Skip("selftest target not present")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "lf")
	build := exec.Command("clang", "-g", "-O1", "-fsanitize=address,fuzzer", "-I", src,
		filepath.Join(src, "asanlab.c"), filepath.Join(src, "asanlab_libfuzzer.c"), "-o", bin)
	if out, err := build.CombinedOutput(); err != nil {
		t.Skipf("libFuzzer build unavailable: %v: %s", err, out)
	}

	// The generic fallback is the interesting one: it carries \x00 and \xff.
	for _, id := range []string{"customer_repo_unknown_id", "mpack", "cjson"} {
		text := hunt.LibFuzzerDictTextForTarget(id)
		if strings.TrimSpace(text) == "" {
			continue
		}
		dictPath := filepath.Join(dir, "d_"+id+".dict")
		if err := os.WriteFile(dictPath, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		corpus := filepath.Join(dir, "c_"+id)
		if err := os.MkdirAll(corpus, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(bin, corpus, "-dict="+dictPath, "-max_total_time=2",
			"-max_len=64", "-detect_leaks=0", "-ignore_crashes=1", "-artifact_prefix="+corpus+"/")
		cmd.Env = append(os.Environ(), "ASAN_OPTIONS=detect_leaks=0:abort_on_error=0")
		out, _ := cmd.CombinedOutput()
		got := string(out)
		if strings.Contains(got, "ParseDictionaryFile") {
			t.Errorf("target %q: libFuzzer rejected the dictionary:\n%s", id, trimOut(got))
			continue
		}
		if !strings.Contains(got, "Dictionary:") {
			t.Errorf("target %q: libFuzzer did not report loading a dictionary:\n%s", id, trimOut(got))
		}
	}
}

func trimOut(s string) string {
	if len(s) > 800 {
		return s[:800] + "…"
	}
	return s
}

func repoRootForHuntTest(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatal("repo root not found")
	return ""
}

// Leak hunting follows the wall, and the env must agree with the libFuzzer flag:
// libFuzzer runs its own LSan pass, and ASAN_OPTIONS has to allow it or the pass
// reports nothing while still costing the shutdown time.
func TestLeakDetectionFollowsTheWall(t *testing.T) {
	for _, wall := range []int{15, 60, 90, 179} {
		if got := hunt.LibFuzzerASANOptionsForWall(wall); !strings.Contains(got, "detect_leaks=0") {
			t.Errorf("wall %ds must keep the merge window: %s", wall, got)
		}
	}
	for _, wall := range []int{180, 210, 300, 600} {
		got := hunt.LibFuzzerASANOptionsForWall(wall)
		if !strings.Contains(got, "detect_leaks=1") {
			t.Errorf("wall %ds should hunt leaks: %s", wall, got)
		}
		for _, keep := range []string{"abort_on_error=1", "halt_on_error=1", "symbolize=0", "allocator_may_return_null=1"} {
			if !strings.Contains(got, keep) {
				t.Errorf("wall %ds lost %s: %s", wall, keep, got)
			}
		}
	}
}
