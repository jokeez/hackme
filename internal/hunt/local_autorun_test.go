package hunt

import "testing"

func TestParseLocalAutorunState(t *testing.T) {
	st := ParseLocalAutorunState(map[string]any{
		"hunt_local_iterations": 4000,
		"hunt_local_done":       true,
		"hunt_local_verdict":    "CLEAN",
	})
	if st.IterationsDone != 4000 || !st.Completed || st.Verdict != "CLEAN" {
		t.Fatalf("st=%+v", st)
	}
}

func TestLocalAutorunStateRoundTrip(t *testing.T) {
	st := LocalAutorunState{IterationsDone: 100, CrashesFound: 2, Verdict: "INFORMATIONAL", StartedAt: 1, LastTickAt: 2}
	m := LocalAutorunStateToSummary(nil, st)
	st2 := ParseLocalAutorunState(m)
	if st2.IterationsDone != 100 || st2.CrashesFound != 2 || st2.Verdict != "INFORMATIONAL" {
		t.Fatalf("st2=%+v", st2)
	}
}

func TestHuntRunOptionsFromConfig(t *testing.T) {
	cfg := map[string]any{"hunt_detect_leaks": true, "mutator_dict": []byte("{")}
	opts := HuntRunOptionsFromConfig(cfg)
	if !opts.DetectLeaks || len(opts.MutatorDict) == 0 {
		t.Fatalf("opts=%+v", opts)
	}
}
