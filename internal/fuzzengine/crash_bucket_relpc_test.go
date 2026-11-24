package fuzzengine

import (
	"math"
	"strconv"
	"testing"
)

// Frame PCs arrive inside miner-submitted crash reports. The relative-offset site is
// what the crash bucket — and therefore PayRun — is keyed on, so a PC the reporter
// chooses must not be able to change the arithmetic.

func TestFormatPCDeltaExactForHighPCs(t *testing.T) {
	cases := []struct {
		name      string
		off, base uint64
		want      string
	}{
		{"zero", 0x1000, 0x1000, "+0x0"},
		{"forward", 0x1040, 0x1000, "+0x40"},
		{"backward", 0x1000, 0x1040, "-0x40"},
		{"above int64 max", math.MaxUint64, math.MaxUint64 - 0x20, "+0x20"},
		{"below from above int64 max", math.MaxUint64 - 0x20, math.MaxUint64, "-0x20"},
		{"straddles the sign bit", 1 << 63, (1 << 63) - 0x10, "+0x10"},
		{"full range apart", math.MaxUint64, 0, "+0xffffffffffffffff"},
		{"full range apart reversed", 0, math.MaxUint64, "-0xffffffffffffffff"},
	}
	for _, tc := range cases {
		if got := formatPCDelta(tc.off, tc.base); got != tc.want {
			t.Errorf("%s: formatPCDelta(%#x, %#x) = %s, want %s", tc.name, tc.off, tc.base, got, tc.want)
		}
	}
}

// The old int64 narrowing turned a high PC into a negative delta. Two frames that are
// 0x20 apart must read as 0x20 apart no matter where in the address space they sit.
func TestRelativeOffsetSiteIndependentOfAddressSpaceRegion(t *testing.T) {
	spans := []uint64{0x400000, 1 << 40, (1 << 63) - 0x100, 1 << 63, math.MaxUint64 - 0x100}
	var sites []string
	for _, base := range spans {
		frames := []crashFrame{
			{module: "lib.so", pc: "0x" + strconv.FormatUint(base, 16)},
			{module: "lib.so", pc: "0x" + strconv.FormatUint(base+0x20, 16)},
			{module: "lib.so", pc: "0x" + strconv.FormatUint(base+0x60, 16)},
		}
		sites = append(sites, relativeOffsetSite(frames))
	}
	first := sites[0]
	if first == "" {
		t.Fatal("relativeOffsetSite returned empty for a three-frame stack")
	}
	for i, s := range sites[1:] {
		if s != first {
			t.Fatalf("same frame spacing produced different sites: base %#x -> %s, base %#x -> %s",
				spans[0], first, spans[i+1], s)
		}
	}
	for _, s := range sites {
		if len(s) > 0 && s[0] == '-' {
			t.Fatalf("site must not start negative: %s", s)
		}
	}
}

// Different spacing must still separate: the fix must not flatten real differences.
func TestRelativeOffsetSiteStillDistinguishesSpacing(t *testing.T) {
	mk := func(step uint64) string {
		base := uint64(math.MaxUint64 - 0x1000)
		return relativeOffsetSite([]crashFrame{
			{module: "lib.so", pc: "0x" + strconv.FormatUint(base, 16)},
			{module: "lib.so", pc: "0x" + strconv.FormatUint(base+step, 16)},
		})
	}
	if a, b := mk(0x20), mk(0x40); a == b {
		t.Fatalf("different frame spacing collapsed into one site: %s", a)
	}
}

// The exploitable shape: narrowing through int64 made the delta wrap, so a reporter
// could pick PCs whose wrapped delta equals an ordinary small offset and land their
// crash in someone else's family — or split one bug across families. Here a pair that
// is 2^64-1 apart used to render exactly like a pair one byte apart.
func TestCraftedPCsCannotCollideWithAnOrdinaryDelta(t *testing.T) {
	crafted := relativeOffsetSite([]crashFrame{
		{module: "lib.so", pc: "0xffffffffffffffff"},
		{module: "lib.so", pc: "0x0"},
	})
	ordinary := relativeOffsetSite([]crashFrame{
		{module: "lib.so", pc: "0x1000"},
		{module: "lib.so", pc: "0x1001"},
	})
	if crafted == ordinary {
		t.Fatalf("a crafted PC pair collides with a one-byte delta: both render %s", crafted)
	}
	if want := "rel:+0x0,-0xffffffffffffffff"; crafted != want {
		t.Fatalf("crafted site = %s, want %s", crafted, want)
	}
	if want := "rel:+0x0,+0x1"; ordinary != want {
		t.Fatalf("ordinary site = %s, want %s", ordinary, want)
	}
	// Same distance, opposite direction, must stay distinguishable too.
	reversed := relativeOffsetSite([]crashFrame{
		{module: "lib.so", pc: "0x0"},
		{module: "lib.so", pc: "0xffffffffffffffff"},
	})
	if reversed == crafted {
		t.Fatalf("direction lost: both render %s", crafted)
	}
}
