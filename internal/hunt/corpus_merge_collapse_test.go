package hunt

import "testing"

// The shared corpus is what the pool compounds and what miners are paid to grow, so a
// -merge=1 result that keeps almost nothing is treated as a failed merge rather than a
// minimization. Observed on the hub: 97 inputs in, 1 out, every lease.
func TestCorpusMergeCollapseGuard(t *testing.T) {
	cases := []struct {
		before, after int
		collapsed     bool
		why           string
	}{
		{97, 1, true, "the hub case"},
		{97, 4, true, "still far below the floor"},
		{97, 8, false, "at the floor"},
		{97, 40, false, "ordinary minimization"},
		{8, 1, false, "tiny corpora are not guarded (merge is skipped below 9 anyway)"},
		{9, 1, true, "just above the skip threshold"},
		{1000, 49, true, "5% floor applies to large corpora"},
		{1000, 50, false, "exactly 5%"},
		{1000, 900, false, "mild dedupe"},
		{0, 0, false, "nothing in, nothing to lose"},
	}
	for _, tc := range cases {
		if got := corpusMergeCollapsed(tc.before, tc.after); got != tc.collapsed {
			t.Errorf("corpusMergeCollapsed(%d, %d) = %v, want %v (%s)", tc.before, tc.after, got, tc.collapsed, tc.why)
		}
	}
}

// The fork wrapper's coverage is its own, not the target's, so nothing that reasons
// about coverage may trust it.
func TestBlindFuzzerRail(t *testing.T) {
	if !blindFuzzerRail("/opt/hackme/.cache/oss-cve-bin/jsmn-abc.bin") {
		t.Error("a session with a stdin binary runs the fork wrapper and is blind")
	}
	for _, in := range []string{"", "   "} {
		if blindFuzzerRail(in) {
			t.Errorf("in-process session must not be treated as blind (%q)", in)
		}
	}
}
