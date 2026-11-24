package fuzzengine

import (
	"strings"
	"testing"
)

// Both reports below are real: one yajl 2.1.0 leak (CVE-2023-33460 class, an
// unwound context stack in yajl_tree_parse's error path) reached twice — once
// through the fork wrapper's stdin binary and once through the in-process rail.
// The library frames are the bug; everything below them is scaffolding, and the
// scaffolding differs per rail. If it reaches the family key, moving a target from
// one rail to the other turns one finding into two — a second payout for one bug.
const leakViaStdinBinary = `==1234==ERROR: LeakSanitizer: detected memory leaks

Direct leak of 24 byte(s) in 1 object(s) allocated from:
    #0 0x5b20341fc1b3 in malloc (/tmp/yajl210_stdin+0xc91b3) (BuildId: 036ea33)
    #1 0x5b2034249373 in context_push /src/.cache/oss-cve-clones/yajl-210/src/yajl_tree.c:117:13
    #2 0x5b2034249373 in handle_start_map /src/.cache/oss-cve-clones/yajl-210/src/yajl_tree.c:339:14
    #3 0x5b203424892b in yajl_tree_parse /src/.cache/oss-cve-clones/yajl-210/src/yajl_tree.c:435:14
    #4 0x5b203423a8ba in main /src/tasks/sources/fuzz/oss/yajl_stdin.c:38:18
    #5 0x741cd682a1c9 in __libc_start_call_main csu/../sysdeps/nptl/libc_start_call_main.h:58:16

SUMMARY: AddressSanitizer: 24 byte(s) leaked in 1 allocation(s).
`

const leakViaInProcessRail = `==1364170==ERROR: LeakSanitizer: detected memory leaks

Direct leak of 24 byte(s) in 1 object(s) allocated from:
    #0 0x5dddb9b14df3 in malloc (/tmp/yajl210_inproc+0x108df3) (BuildId: 3240d36)
    #1 0x5dddb9b665e6 in context_push /src/.cache/oss-cve-clones/yajl-210/src/yajl_tree.c:117:13
    #2 0x5dddb9b665e6 in handle_start_map /src/.cache/oss-cve-clones/yajl-210/src/yajl_tree.c:339:14
    #3 0x5dddb9b659a6 in yajl_tree_parse /src/.cache/oss-cve-clones/yajl-210/src/yajl_tree.c:435:14
    #4 0x5dddb9b5367b in hackme_stdin_main /src/tasks/sources/fuzz/oss/yajl_stdin.c:38:18
    #5 0x5dddb9b53419 in LLVMFuzzerTestOneInput /src/tasks/sources/fuzz/benchmark/stdin_inproc_libfuzzer.c:33:8
    #6 0x5dddb9a60cc4 in fuzzer::Fuzzer::ExecuteCallback(unsigned char const*, unsigned long) (/tmp/yajl210_inproc+0x54cc4)
    #7 0x5dddb9a49df6 in fuzzer::RunOneTest(fuzzer::Fuzzer*, char const*, unsigned long) (/tmp/yajl210_inproc+0x3ddf6)
    #8 0x5dddb9a7a066 in main (/tmp/yajl210_inproc+0x6e066)

SUMMARY: AddressSanitizer: 24 byte(s) leaked in 1 allocation(s).
`

func TestHarnessFramesAreNotPartOfTheBugIdentity(t *testing.T) {
	for name, msg := range map[string]string{
		"stdin binary": leakViaStdinBinary,
		"in-process":   leakViaInProcessRail,
	} {
		site := StableBugSite(msg)
		if site == "" {
			t.Fatalf("%s: empty bug site", name)
		}
		for _, scaffolding := range []string{
			"main@", "hackme_stdin_main", "llvmfuzzertestoneinput", "fuzzer::", "__libc_start",
		} {
			if strings.Contains(site, scaffolding) {
				t.Errorf("%s: %q leaked into the bug identity: %s", name, scaffolding, site)
			}
		}
		if !strings.Contains(site, "context_push@") || !strings.Contains(site, "handle_start_map@") {
			t.Errorf("%s: the library frames must stay: %s", name, site)
		}
	}
}

func TestHarnessFuncClassification(t *testing.T) {
	for _, in := range []string{
		"main", "Main", "hackme_stdin_main", "LLVMFuzzerTestOneInput", "LLVMFuzzerRunDriver",
		"fuzzer::Fuzzer::ExecuteCallback(unsigned char const*, unsigned long)", "fuzzer::RunOneTest",
	} {
		if !harnessFunc(in) {
			t.Errorf("%q is scaffolding", in)
		}
	}
	for _, in := range []string{
		"context_push", "handle_start_map", "yajl_tree_parse", "xmlParseDocument",
		"mainloop", "domain_check", "", "fuzzer_state_init",
	} {
		if harnessFunc(in) {
			t.Errorf("%q is target code and must stay in the identity", in)
		}
	}
}

// What this fix does not solve, recorded so it is not mistaken for solved: the two
// rails inline differently (yajl_do_parse survives in one build and is folded into
// its caller in the other), so the frame lists still differ by that frame and the two
// reports remain two families. Narrowing the key to the top frames would merge them,
// but that changes payout granularity for every finding, so it is the operator's call.
func TestRailsStillDifferByInlining(t *testing.T) {
	withInlinedFrame := strings.Replace(leakViaInProcessRail,
		"    #3 0x5dddb9b659a6 in yajl_tree_parse",
		"    #3 0x5dddb9b5b9b1 in yajl_do_parse /src/.cache/oss-cve-clones/yajl-210/src/yajl_parser.c:269:25\n    #4 0x5dddb9b659a6 in yajl_tree_parse", 1)
	a := FindingFamily("crash", leakViaStdinBinary)
	b := FindingFamily("crash", withInlinedFrame)
	if a == b {
		t.Log("inlining no longer splits the family — if this is intentional, drop this test")
	}
	// The harness filter must at least make the non-inlined pair identical.
	if got, want := FindingFamily("crash", leakViaInProcessRail), a; got != want {
		t.Errorf("same bug, same frames, different family:\n in-process: %s\n stdin:      %s", got, want)
	}
}
