package main

import (
	"strings"
	"testing"
)

// The verdict logic is what makes this a known-answer test rather than a smoke test, so
// it is worth pinning on its own: a fuzzer run is too slow and too variable to assert on.
func TestCheckSelftest(t *testing.T) {
	const want = defaultExpectBucket

	// The pipeline reached the bug and bucketed it the same way: the only pass.
	if err := checkSelftest(selftestResult{Crashes: 1088, Buckets: []string{want}}, 1, want); err != nil {
		t.Fatalf("a healthy run was rejected: %v", err)
	}

	// No crashes at all. This is the failure that matters most: a rail that silently
	// stopped carrying the target looks exactly like a clean run.
	err := checkSelftest(selftestResult{Crashes: 0}, 1, want)
	if err == nil {
		t.Fatal("zero crashes on a target with a known bug was treated as success")
	}

	// Crashes but nothing bucketable — the bucketer or the replay broke.
	if err := checkSelftest(selftestResult{Crashes: 5}, 1, want); err == nil {
		t.Fatal("unbucketable crashes were treated as success")
	}

	// The bucket key moved. Harmless-looking, and it re-prices every past finding: one
	// bug under a new key is a new bug, so the same leak gets paid twice.
	err = checkSelftest(selftestResult{Crashes: 5, Buckets: []string{"hygiene|lsan|leak|value_alloc@yajl_tree.c:64:9"}}, 1, want)
	if err == nil {
		t.Fatal("a changed bucket key was treated as success")
	}

	// An empty expectation means "only require crashes" — used when adding a new control
	// before its answer is known.
	if err := checkSelftest(selftestResult{Crashes: 2, Buckets: []string{"anything"}}, 1, ""); err != nil {
		t.Fatalf("empty expectation should skip the bucket check: %v", err)
	}

	// min-crashes above what was found must fail even when the bucket matches.
	if err := checkSelftest(selftestResult{Crashes: 1, Buckets: []string{want}}, 10, want); err == nil {
		t.Fatal("min-crashes was not enforced")
	}
}

// The first real run of this control failed with "found 0 crash artifacts" on a 90 s
// wall — not because the pipeline had broken, but because libFuzzer runs with
// -detect_leaks=0 below hunt.LeakDetectWallSec, so a leak expectation could never fire.
// A control that can report a false failure teaches the operator to ignore it.
func TestIsLeakBucket(t *testing.T) {
	for _, b := range []string{
		defaultExpectBucket,
		"hygiene|lsan|leak|value_alloc@x.c:1:1",
		"HYGIENE|LSAN|LEAK|y",
	} {
		if !isLeakBucket(b) {
			t.Errorf("isLeakBucket(%q) = false, want true", b)
		}
	}
	for _, b := range []string{
		"",
		"crash|asan|heap-buffer-overflow|parse@x.c:1:1",
		"crash|generic",
		"native_silent_exit|abort",
	} {
		if isLeakBucket(b) {
			t.Errorf("isLeakBucket(%q) = true, want false", b)
		}
	}
}

// Several real bugs in one target must not make the control fail: yajl 2.1.0 yields at
// least two distinct leak families (value_alloc and context_push), and asserting on
// whichever artifact sorted first compared against an arbitrary one of them.
func TestCheckSelftestFindsExpectationAmongSeveral(t *testing.T) {
	const want = defaultExpectBucket
	res := selftestResult{Crashes: 115, Buckets: []string{
		"crash|f484b7b2",
		"hygiene|lsan|leak|context_push@yajl_tree.c:121:5",
		want,
	}}
	if err := checkSelftest(res, 1, want); err != nil {
		t.Fatalf("expectation present among several families was rejected: %v", err)
	}
	gone := selftestResult{Crashes: 115, Buckets: []string{
		"crash|f484b7b2",
		"hygiene|lsan|leak|context_push@yajl_tree.c:121:5",
	}}
	err := checkSelftest(gone, 1, want)
	if err == nil {
		t.Fatal("a missing expectation was treated as success")
	}
	if !strings.Contains(err.Error(), "context_push") {
		t.Fatalf("error %q does not say what was found instead", err)
	}
}

// The family key carries the whole call path, so one missing free reached by several
// parse paths yields several families. Measured on yajl 2.1.0: a single leak split into
// six, e.g. value_alloc reached via handle_number at two different parser offsets, via
// handle_string and via handle_start_map. The control must accept those as the same
// known bug — matching the allocation site, which is the stable leading frame — or it
// fails every time a new path into an old bug turns up, which is not a regression.
func TestCheckSelftestMatchesAllocationSitePrefix(t *testing.T) {
	const want = defaultExpectBucket
	full := want + ",handle_number@yajl_tree.c:297:9,yajl_do_parse@yajl_parser.c:282:29"
	if err := checkSelftest(selftestResult{Crashes: 115, Buckets: []string{full}}, 1, want); err != nil {
		t.Fatalf("a full-path family with the expected allocation site was rejected: %v", err)
	}
	// A different allocation site is a real change and must still fail.
	other := "hygiene|lsan|leak|context_push@yajl_tree.c:117:13,handle_start_array@yajl_tree.c:364:14"
	if err := checkSelftest(selftestResult{Crashes: 115, Buckets: []string{other}}, 1, want); err == nil {
		t.Fatal("a different allocation site was accepted as the known bug")
	}
}
