package main

import "testing"

func TestFilterPublicOfflineWorkers(t *testing.T) {
	now := int64(1_700_000_000)
	in := map[string]workerPayoutStat{
		"live": {
			Online:       true,
			LastSeenUnix: now - 10,
			PayoutHMC:    1,
		},
		"recent-off": {
			Online:       false,
			LastSeenUnix: now - 3600, // 1h
			PayoutHMC:    2,
		},
		"ancient": {
			Online:       false,
			LastSeenUnix: now - 400*3600, // 400h
			PayoutHMC:    99,
		},
		"never-seen": {
			Online:    false,
			PayoutHMC: 0.01,
		},
	}
	out := filterPublicOfflineWorkers(in, now, 6*3600)
	if len(out) != 2 {
		t.Fatalf("len=%d want 2 (live+recent); got %#v", len(out), out)
	}
	if _, ok := out["live"]; !ok {
		t.Fatal("live missing")
	}
	if _, ok := out["recent-off"]; !ok {
		t.Fatal("recent offline missing")
	}
	if _, ok := out["ancient"]; ok {
		t.Fatal("ancient offline must be hidden from public map")
	}
	if _, ok := out["never-seen"]; ok {
		t.Fatal("never-seen must be hidden")
	}
}
