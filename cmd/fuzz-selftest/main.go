// fuzz-selftest proves the fuzzing pipeline still finds a bug it is known to find.
//
// It runs the production hunt session against a catalog target pinned to a version with a
// known defect, then replays one crash artifact with symbolization and compares the stable
// bucket against the expected one. A known-answer test, which is the only kind that
// catches the failures this pipeline actually has:
//
//   - a rail that silently stops carrying the target (a missing .kind sidecar would have
//     kept the in-process rail off in production forever, and nothing would have said so);
//   - a dictionary or corpus regression that quietly drops the fuzzer back to blind input;
//   - a change to the bucket key, which decides whether one bug is paid once or a thousand
//     times — yajl 2.1.0 produces ~1000 artifacts for a single leak.
//
// Default target is yajl-210 (yajl 2.1.0, the CVE-2023-33460 leak family), which this
// engine reaches within seconds, so a short wall is enough to be meaningful.
//
// Exit 0 when the expectation holds, 1 when it does not. Meant for a nightly ops run.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"hackme/internal/fuzzengine"
	"hackme/internal/fuzzupstream"
	"hackme/internal/hunt"
)

// defaultExpectBucket is the bucket yajl 2.1.0's leak lands in. The key is the allocation
// site, not the caller, so handle_number and handle_string — two reachable paths into the
// same missing cleanup — stay one finding.
const defaultExpectBucket = "hygiene|lsan|leak|value_alloc@yajl_tree.c:63:9"

type selftestResult struct {
	Crashes int
	Buckets []string // distinct buckets seen across the replayed sample
	Corpus  int
}

// checkSelftest decides the verdict. Split out so it is testable without a fuzzer.
func checkSelftest(res selftestResult, minCrashes int, expectBucket string) error {
	if res.Crashes < minCrashes {
		return fmt.Errorf("found %d crash artifacts, expected at least %d — the pipeline no longer reaches a bug it is known to reach",
			res.Crashes, minCrashes)
	}
	want := strings.TrimSpace(expectBucket)
	if want == "" {
		return nil
	}
	if len(res.Buckets) == 0 {
		return fmt.Errorf("crashes were found but none could be bucketed; expected %q", want)
	}
	// The expectation is matched as a PREFIX of a family, not as the whole key. The key
	// carries the full call path, so one missing free reached by handle_number,
	// handle_string and handle_start_map produces three different families — measured on
	// yajl 2.1.0, where a single leak split into six. Pinning the whole key here would
	// make the control fail every time a new path into the same bug is discovered, which
	// is not a regression. The leading frame — the allocation site — is the stable part
	// and the thing worth asserting.
	for _, b := range res.Buckets {
		if strings.HasPrefix(strings.TrimSpace(b), want) {
			return nil
		}
	}
	return fmt.Errorf("no family starts with %q; found %d: %s — the allocation site moved, which re-prices every past finding",
		want, len(res.Buckets), strings.Join(res.Buckets, " | "))
}

func main() {
	target := flag.String("target", "yajl-210", "catalog target id with a known defect")
	wall := flag.Int("wall", 240, "wall seconds for the session")
	minCrashes := flag.Int("min-crashes", 1, "fail below this many crash artifacts")
	expect := flag.String("expect-bucket", defaultExpectBucket, "expected stable bucket (empty skips the check)")
	sample := flag.Int("sample", 12, "crash artifacts to replay and bucket")
	flag.Parse()

	repoRoot := hunt.RepoRoot()
	m, err := fuzzupstream.LoadManifest(repoRoot)
	if err != nil {
		fail("manifest: %v", err)
	}
	known := false
	for i := range m.Targets {
		if m.Targets[i].ID == *target {
			known = true
			break
		}
	}
	if !known {
		fail("target %q is not in the catalog", *target)
	}

	// A control that cannot reach its own expectation is worse than no control: it
	// reports a broken pipeline when nothing is broken, and an operator who sees that
	// once stops believing the next one. Leak findings need -detect_leaks=1, which
	// libFuzzer only gets at or above this wall.
	if isLeakBucket(*expect) && *wall < hunt.LeakDetectWallSec {
		fail("expected bucket %q is a leak, but a %ds wall runs with -detect_leaks=0 (needs >= %ds) — raise -wall",
			*expect, *wall, hunt.LeakDetectWallSec)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*wall+900)*time.Second)
	defer cancel()
	start := time.Now()
	if _, err := hunt.RunPersistentLibFuzzerSession(ctx, repoRoot, *target, *wall); err != nil {
		// A session error is itself a failure: the control must run, not just not crash.
		fail("session: %v", err)
	}
	elapsed := time.Since(start).Round(time.Second)

	corpusDir := hunt.PersistentLibFuzzerCorpusDir(repoRoot, *target)
	res := selftestResult{Corpus: countFiles(corpusDir)}
	crashes := findCrashes(filepath.Join(corpusDir, ".hunt-run", "crashes"))
	res.Crashes = len(crashes)
	seen := map[string]bool{}
	for i, c := range crashes {
		if i >= *sample {
			break
		}
		if b := strings.TrimSpace(bucketOf(repoRoot, *target, c)); b != "" && !seen[b] {
			seen[b] = true
			res.Buckets = append(res.Buckets, b)
		}
	}
	sort.Strings(res.Buckets)

	fmt.Printf("[fuzz-selftest] target=%s wall=%ds elapsed=%s corpus=%d crashes=%d\n",
		*target, *wall, elapsed, res.Corpus, res.Crashes)
	for _, b := range res.Buckets {
		fmt.Printf("[fuzz-selftest] family: %s\n", b)
	}
	if err := checkSelftest(res, *minCrashes, *expect); err != nil {
		fail("%v", err)
	}
	fmt.Println("[fuzz-selftest] PASS — the pipeline still finds the known bug and buckets it the same way")
}

// bucketOf replays one artifact with symbolization on. The session itself runs with
// symbolize=0 (ASan must exit fast so libFuzzer writes the artifact), so the stack with
// file:line only exists on a deliberate replay.
func bucketOf(repoRoot, target, artifact string) string {
	bin := hunt.LibFuzzerImportBinPath(repoRoot, target)
	if st, err := os.Stat(bin); err != nil || st.IsDir() {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, artifact)
	cmd.Env = append(os.Environ(),
		"ASAN_OPTIONS=detect_leaks=1:symbolize=1:abort_on_error=0:exitcode=0")
	out, _ := cmd.CombinedOutput()
	return fuzzengine.StableCrashBucket("native_crash", string(out))
}

func findCrashes(dir string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "crash-") {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func countFiles(dir string) int {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[fuzz-selftest] FAIL — "+format+"\n", args...)
	os.Exit(1)
}

// isLeakBucket reports whether the expectation is a LeakSanitizer finding, which only
// appears when libFuzzer runs with leak detection on.
func isLeakBucket(bucket string) bool {
	b := strings.ToLower(bucket)
	return strings.Contains(b, "|leak|") || strings.Contains(b, "|lsan|")
}
