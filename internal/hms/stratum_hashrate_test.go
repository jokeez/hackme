package hms

import (
	"testing"
	"time"
)

func TestHashrateIgnoresPassword(t *testing.T) {
	reg := NewStratumRegistry()
	id := reg.Connect("1.2.3.4:1", "rig1")
	reg.Touch(id, "rig1", "th=999")
	_, th, peers := reg.Snapshot(nil)
	if th != 0 {
		t.Fatalf("password must not set hashrate: %v", th)
	}
	if len(peers) != 1 || peers[0]["hashrate_th"].(float64) != 0 {
		t.Fatalf("peers=%+v", peers)
	}
}

func TestMeasuredHashrateFromSubmits(t *testing.T) {
	reg := NewStratumRegistry()
	id := reg.Connect("1.2.3.4:1", "rig1")
	reg.Touch(id, "rig1", "th=999")
	now := time.Now().Unix()
	for i := 0; i < minSubmitsForHashrate+5; i++ {
		reg.RecordSubmit(id)
	}
	p := reg.peers[id]
	p.SubmitTimes = nil
	for i := 0; i < 30; i++ {
		p.SubmitTimes = appendRecentSubmit(p.SubmitTimes, now-int64(i))
	}
	th := measuredPeerTH(p, now)
	if th <= 0 {
		t.Fatalf("expected measured th > 0, got %v", th)
	}
}

func TestSnapshotAggregatesWorkerName(t *testing.T) {
	reg := NewStratumRegistry()
	id1 := reg.Connect("1.2.3.4:1", "farm.rigA")
	id2 := reg.Connect("1.2.3.4:2", "farm.rigA")
	reg.Touch(id1, "farm.rigA", "th=110")
	reg.Touch(id2, "farm.rigA", "th=110")
	now := time.Now().Unix()
	for i := 0; i < 20; i++ {
		reg.RecordSubmit(id1)
	}
	p := reg.peers[id1]
	p.SubmitTimes = nil
	for i := 0; i < 20; i++ {
		p.SubmitTimes = appendRecentSubmit(p.SubmitTimes, now-int64(i))
	}
	reg.RecordShare(id1, true)
	n, th, peers := reg.Snapshot(nil)
	if n != 1 {
		t.Fatalf("connected=%d want 1 aggregated worker", n)
	}
	if len(peers) != 1 || peers[0]["worker_id"] != "farm.rigA" {
		t.Fatalf("peers=%+v", peers)
	}
	if th <= 0 {
		t.Fatalf("measured th=%v want >0 from submits not password", th)
	}
}
