package main

import (
	"encoding/json"
	"testing"
)

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
