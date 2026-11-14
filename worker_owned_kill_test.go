package main

import "testing"

func TestWorkerIDOwned(t *testing.T) {
	ids := []string{"worker-kapa-pc"}
	cases := map[string]bool{
		"worker-kapa-pc":        true,
		"worker-kapa-pc-2":      true,
		"worker-kapa-pc-fuzz":   true,
		"worker-kapa-pc-cuda":   false,
		"worker-kapa-pc-cuda0":  true,
		"worker-kapa-pc-ocl1":   true,
		"worker-kapa":           false,
		"worker-anechka":        false,
		"worker-kapa-pc-abc123": false,
		"":                      false,
	}
	for got, want := range cases {
		if workerIDOwned(got, ids) != want {
			t.Errorf("workerIDOwned(%q) = %v, want %v", got, !want, want)
		}
	}
	if ownedByWorkerIDs([]string{"", "  "}) != nil {
		t.Fatal("no known worker id must mean: signal nothing")
	}
}
