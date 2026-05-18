package lanpool

import (
	"testing"
	"time"
)

func TestRealNetworkStats_SumsGH(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Upsert("10.0.0.1:1", PushWorkBody{WorkerID: "w1", Name: "a", HashrateGHS: 1000})
	local := LocalMining{Running: true, AttemptsPerSec: 1e9, GPUTotalGHS: 0}
	r := RealNetworkStats(reg, "HMC-node", local)
	if r.GlobalMock {
		t.Fatal("expected real stats")
	}
	if r.GlobalHashrateTHS < 1.9 {
		t.Fatalf("want ~2 TH/s aggregate, got %v", r.GlobalHashrateTHS)
	}
	if r.PeerConnections != 1 {
		t.Fatalf("peer links: %d", r.PeerConnections)
	}
}

func TestRealNetworkStats_ExcludesOfflineFromActiveRigs(t *testing.T) {
	reg := NewRegistry()
	_ = reg.Upsert("10.0.0.1:1", PushWorkBody{WorkerID: "w-online", Name: "online", HashrateGHS: 100})
	_ = reg.Upsert("10.0.0.2:1", PushWorkBody{WorkerID: "w-offline", Name: "offline", HashrateGHS: 200})
	reg.SetRigLastSeen("w-offline", time.Now().Add(-2*time.Minute))

	r := RealNetworkStats(reg, "HMC-node", LocalMining{})
	if r.PeerConnections != 1 {
		t.Fatalf("peer links: want 1, got %d", r.PeerConnections)
	}
	if len(r.ActiveRigs) != 1 {
		t.Fatalf("active rigs: want 1, got %d", len(r.ActiveRigs))
	}
	if got := r.ActiveRigs[0].WorkerID; got != "w-online" {
		t.Fatalf("active rig id: want w-online, got %s", got)
	}
}
