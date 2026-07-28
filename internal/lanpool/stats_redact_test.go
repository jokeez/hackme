package lanpool

import "testing"

func TestRedactPublicNetworkStats(t *testing.T) {
	in := NetworkStatsResponse{
		ActiveRigs: []NetworkRigRow{{
			MetricsRow: MetricsRow{WorkerID: "w1", IP: "203.0.113.1"},
		}},
	}
	out := RedactPublicNetworkStats(in)
	if out.ActiveRigs[0].IP != "" {
		t.Fatalf("want redacted ip, got %q", out.ActiveRigs[0].IP)
	}
}
