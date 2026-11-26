package poolfuzz

import (
	"testing"

	"hackme/internal/hunt"
)

// A reclaim lease shorter than the engine wall hands a still-running worker's range to
// somebody else: the finished shard is discarded and the range comes back as
// "already closed" or "leased to another worker". Raising a wall without raising the
// reclaim cap does exactly that, silently, so the relationship is asserted here rather
// than left to a comment.
func TestReclaimLeaseOutlivesEveryWall(t *testing.T) {
	walls := map[string]int{
		"lite":     hunt.PoolEngineLeaseSecLite,
		"standard": hunt.PoolEngineLeaseSecStandard,
		"heavy":    hunt.PoolEngineLeaseSecHeavy,
		"default":  hunt.DefaultLibFuzzerLeaseSec,
		"max":      hunt.MaxEngineLeaseSec,
	}
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_LEASE_SEC", "")
	for name, wall := range walls {
		if wall <= 0 {
			t.Fatalf("%s wall must be positive, got %d", name, wall)
		}
		cfg := map[string]any{
			"work_kind":        "hunt_shard",
			"hunt_engine":      "libfuzzer",
			"engine_lease_sec": float64(wall),
			"pool_distributed": true,
		}
		lease := leaseSecondsForConfig(cfg)
		if lease <= int64(wall) {
			t.Errorf("%s: reclaim lease %ds does not outlive the %ds wall — a live worker's range would be reclaimed mid-run",
				name, lease, wall)
		}
	}
}

// The cap is what actually bounds the lease, so it has to clear the longest wall too.
func TestReclaimCapClearsLongestWall(t *testing.T) {
	longest := hunt.PoolEngineLeaseSecHeavy
	for _, w := range []int{hunt.PoolEngineLeaseSecLite, hunt.PoolEngineLeaseSecStandard, hunt.MaxEngineLeaseSec} {
		if w > longest {
			longest = w
		}
	}
	if hunt.PoolReclaimLeaseCapSec <= longest {
		t.Fatalf("reclaim cap %ds must exceed the longest wall %ds", hunt.PoolReclaimLeaseCapSec, longest)
	}
}

// Lite used to sit exactly on the leak-detection threshold, so leak detection turned on
// as the run ended. Every pool wall must leave real time above it.
func TestWallsLeaveRoomForLeakDetection(t *testing.T) {
	for name, wall := range map[string]int{
		"lite":     hunt.PoolEngineLeaseSecLite,
		"standard": hunt.PoolEngineLeaseSecStandard,
		"heavy":    hunt.PoolEngineLeaseSecHeavy,
	} {
		if wall <= hunt.LeakDetectWallSec {
			t.Errorf("%s wall %ds is not above LeakDetectWallSec %ds — leak detection would never get time to run",
				name, wall, hunt.LeakDetectWallSec)
		}
	}
}
