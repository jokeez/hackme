package main

import (
	"encoding/json"
	"testing"
)

func TestPoolListingHashrateAndMiners(t *testing.T) {
	hr, wc := poolListingHashrateAndMiners(map[string]any{
		"hashrate_hs":       1.5e12,
		"pool_hashrate_gh_s": 0.1,
		"active_rigs":       []any{map[string]any{"name": "a"}, map[string]any{"name": "b"}},
	})
	if hr != 1.5e12 {
		t.Fatalf("hashrate=%v want 1.5e12", hr)
	}
	if wc != 2 {
		t.Fatalf("miners=%d want 2", wc)
	}
	hr2, wc2 := poolListingHashrateAndMiners(map[string]any{
		"pool_hashrate_gh_s": 3.0,
		"miners":             7,
	})
	if hr2 != 3e9 {
		t.Fatalf("hashrate from GH/s=%v want 3e9", hr2)
	}
	if wc2 != 7 {
		t.Fatalf("miners=%d want 7", wc2)
	}
}

func TestParseJSONUint64(t *testing.T) {
	cases := []struct {
		in   any
		want uint64
		ok   bool
	}{
		{float64(42922), 42922, true},
		{json.Number("100"), 100, true},
		{int64(5), 5, true},
		{uint64(9), 9, true},
		{float64(0), 0, false},
		{"nope", 0, false},
	}
	for _, c := range cases {
		got, ok := parseJSONUint64(c.in)
		if ok != c.ok || got != c.want {
			t.Fatalf("parseJSONUint64(%v) = %d,%v want %d,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}
