package hms

import (
	"testing"
)

func TestStratumRegistrySharesByWorker(t *testing.T) {
	reg := NewStratumRegistry()
	id1 := reg.Connect("1.2.3.4:1", "asic-a")
	id2 := reg.Connect("1.2.3.4:2", "asic-b")
	reg.Touch(id1, "asic-a", "x")
	reg.Touch(id2, "asic-b", "th=100")
	reg.RecordShare(id1, true)
	reg.RecordShare(id1, true)
	reg.RecordShare(id2, false)
	got := reg.SharesByWorker()
	if got["asic-a"] != [2]uint64{2, 0} {
		t.Fatalf("asic-a=%v", got["asic-a"])
	}
	if got["asic-b"] != [2]uint64{0, 1} {
		t.Fatalf("asic-b=%v", got["asic-b"])
	}
}

func TestParseReportedTHVariants(t *testing.T) {
	if parseReportedTH("x") != 0 {
		t.Fatal("x should be 0")
	}
	if parseReportedTH("th=120") != 120 {
		t.Fatal("th=120")
	}
	if parseReportedTH("1.5th") != 1.5 {
		t.Fatal("1.5th")
	}
}
