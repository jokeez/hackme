package main

import (
	"fmt"
	"strings"
	"testing"
)

// The soak watch held HARD on reject_pct=9% (117 of 1303 relayed founds refused) with no
// way to tell a height two miners both found — which costs the miner nothing, since the
// lease's shares stay payable — from a defect. The reason was available at the rejection
// and thrown away. These counters keep it, and must stay bounded: the reason text comes
// from the chain host's reply, so an unexpected one must not grow the stats payload.
func TestNoteBoundedReason(t *testing.T) {
	var m map[string]uint64

	noteBoundedReason(&m, "stale_tip")
	noteBoundedReason(&m, "stale_tip")
	noteBoundedReason(&m, "  stale_tip  ")
	if got := m["stale_tip"]; got != 3 {
		t.Fatalf("stale_tip = %d, want 3 (trimmed variants fold together)", got)
	}

	noteBoundedReason(&m, "")
	if got := m["unspecified"]; got != 1 {
		t.Fatalf("empty reason = %d under \"unspecified\", want 1", got)
	}

	for i := 0; i < baselineRelayReasonMax*3; i++ {
		noteBoundedReason(&m, fmt.Sprintf("reason_%d", i))
	}
	if len(m) > baselineRelayReasonMax+1 {
		t.Fatalf("map grew to %d keys, want at most %d (+other)", len(m), baselineRelayReasonMax+1)
	}
	if m["other"] == 0 {
		t.Fatal("overflow reasons were dropped instead of folding into \"other\"")
	}

	// A nil destination must not panic: the call sites run inside submit handling.
	noteBoundedReason(nil, "stale_tip")
}

// The stats payload must hand out a copy: the JSON encoder runs after m.mu is released,
// so a live map would be read while the next submit writes it.
func TestCopyReasonMapIsDetached(t *testing.T) {
	src := map[string]uint64{"stale_tip": 2}
	cp := copyReasonMap(src)
	src["stale_tip"] = 99
	src["new"] = 1
	if cp["stale_tip"] != 2 || len(cp) != 1 {
		t.Fatalf("copy %v tracked later writes to the source", cp)
	}
	if got := copyReasonMap(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil source gave %v, want an empty non-nil map", got)
	}
}

// The reject reason comes from the chain host's reply body and the counters are served by
// /api/work/stats, which needs no token. Two things follow: the text must not reach a
// public key unbounded, and it must collapse to a CLASS, or per-submit operands (an
// echoed miner address, an HTTP code) spend the whole key budget on one failure mode and
// the counter goes blind just when a new one appears.
func TestNormalizeRelayReason(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"stale_tip_before_baseline_relay", "stale_tip_before_baseline_relay"},
		{"  STALE_TIP  ", "stale_tip"},
		{"", "unspecified"},
		{"\n\t", "unspecified"},
		{`chain: invalid miner_address "HMC-deadbeefdeadbeef"`, "chain: invalid miner_address _"},
		{"relay_http_429", "relay_http_N"},
		{"relay_http_503", "relay_http_N"},
	} {
		if got := normalizeRelayReason(c.in); got != c.want {
			t.Errorf("normalizeRelayReason(%q) = %q, want %q", c.in, got, c.want)
		}
	}

	// An operand-bearing reason must not multiply keys.
	var m map[string]uint64
	for _, addr := range []string{"HMC-aaaa", "HMC-bbbb", "HMC-cccc"} {
		noteBoundedReason(&m, normalizeRelayReason(`chain: invalid miner_address "`+addr+`"`))
	}
	if len(m) != 1 {
		t.Fatalf("three addresses produced %d keys (%v), want one class", len(m), m)
	}

	// A hostile reason cannot install a long public key.
	long := normalizeRelayReason(strings.Repeat("A", 4096))
	if len([]rune(long)) > relayReasonMaxLen {
		t.Fatalf("reason kept %d runes, want at most %d", len([]rune(long)), relayReasonMaxLen)
	}
	if strings.ContainsAny(long, "\n\r\t") {
		t.Fatalf("control characters survived: %q", long)
	}
}
