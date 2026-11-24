package poolfuzz

import (
	"testing"

	"hackme/internal/hunt"
	"hackme/internal/sandbox"
)

func TestPoolExecPerUnitCap(t *testing.T) {
	t.Setenv("HACKME_POOL_EXEC_PER_UNIT_CAP", "")
	cfg := map[string]any{
		"pool_distributed": true,
		"exec_per_unit":    512,
	}
	if got := PoolExecPerUnit(cfg); got != poolExecPerUnitCap {
		t.Fatalf("pool cap: got %d want %d", got, poolExecPerUnitCap)
	}
	cfg["pool_distributed"] = false
	if got := PoolExecPerUnit(cfg); got != 512 {
		t.Fatalf("local uncapped: got %d want 512", got)
	}
}

func TestPoolExecPerUnitCapEnvOverride(t *testing.T) {
	t.Setenv("HACKME_POOL_EXEC_PER_UNIT_CAP", "256")
	cfg := map[string]any{
		"pool_distributed": true,
		"exec_per_unit":    512,
	}
	if got := PoolExecPerUnit(cfg); got != 256 {
		t.Fatalf("env cap: got %d want 256", got)
	}
}

func TestLeaseSecondsForConfigScalesWithExec(t *testing.T) {
	cfg := map[string]any{
		"pool_distributed": true,
		"exec_per_unit":    64,
	}
	sec := leaseSecondsForConfig(cfg)
	timeoutMS := sandbox.Policy().CheckTimeoutMS
	if timeoutMS <= 0 {
		timeoutMS = 300
	}
	wantMin := int64((64*int(timeoutMS))/1000 + 90)
	if wantMin < 90 {
		wantMin = 90
	}
	if sec < wantMin {
		t.Fatalf("lease %d too short for 64 exec (want >= %d)", sec, wantMin)
	}
	if sec > 600 {
		t.Fatal("lease must be capped at 600s")
	}
}

func TestLeaseSecondsForHuntShardUsesIterations(t *testing.T) {
	// Force byte path — coverage engines use engine_lease_sec (not L1 iter × timeout).
	cfg := map[string]any{
		"work_kind":            "hunt_shard",
		"hunt_engine":          "byte",
		"iterations_per_shard": 128,
		"pool_distributed":     true,
		"exec_per_unit":        1,
	}
	sec := leaseSecondsForConfig(cfg)
	wantMin := int64((128*huntExecTimeoutMS)/1000 + 60)
	if wantMin > 360 {
		wantMin = 360
	}
	if sec < wantMin && wantMin < 360 {
		t.Fatalf("hunt lease %d too short for 128 iter (want >= %d)", sec, wantMin)
	}
	if sec > 360 {
		t.Fatalf("hunt lease must be capped at 360s, got %d", sec)
	}
}

func TestLeaseSecondsHuntHeavyCappedAt360(t *testing.T) {
	// Byte heavy: iter×timeout hits the 360s Hunt cap. LF fleet default is ~180 (2×60).
	cfg := map[string]any{
		"work_kind":            "hunt_shard",
		"hunt_engine":          "byte",
		"iterations_per_shard": 256,
		"pool_distributed":     true,
	}
	if sec := leaseSecondsForConfig(cfg); sec != 360 {
		t.Fatalf("heavy hunt lease=%d want 360", sec)
	}
}

func TestLeaseSecondsHuntLibFuzzerFleet(t *testing.T) {
	// Hermetic: parent shells / CI may set HACKME_HUNT_COVERAGE_ENGINE=0 (byte path → 204).
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_LEASE_SEC", "")
	cfg := map[string]any{
		"work_kind":        "hunt_shard",
		"hunt_engine":      "libfuzzer",
		"pool_distributed": true,
	}
	// Unstamped LF wall is DefaultLibFuzzerLeaseSec (180) → max(360, 300)=360.
	if sec := leaseSecondsForConfig(cfg); sec != 360 {
		t.Fatalf("LF fleet lease=%d want 360", sec)
	}
}

func TestLeaseSecondsHuntHeavyWallKeepsMergeSlack(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_LEASE_SEC", "")
	cfg := map[string]any{
		"work_kind":        "hunt_shard",
		"hunt_engine":      "libfuzzer",
		"engine_lease_sec": 210,
		"pool_distributed": true,
	}
	// 2×210=420, under the 480s coverage cap, above the old 360s clip.
	if sec := leaseSecondsForConfig(cfg); sec != 420 {
		t.Fatalf("heavy LF lease=%d want 420", sec)
	}
}

func TestStoredEngineLeaseStaysAndCoverageLeaseCaps(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	// Env would restamp a live campaign to the heavy wall. Stored 90s wins.
	t.Setenv("HACKME_HUNT_ENGINE_LEASE_SEC", "300")
	stored := map[string]any{
		"work_kind":        "hunt_shard",
		"hunt_engine":      "libfuzzer",
		"hunt_package":     "hunt_heavy",
		"engine_lease_sec": 90,
		"pool_distributed": true,
	}
	if got := hunt.EngineLeaseSecFromConfig(stored, "libfuzzer"); got != 90 {
		t.Fatalf("stored wall=%d want 90", got)
	}
	if sec := leaseSecondsForConfig(stored); sec != 210 {
		t.Fatalf("stored reclaim=%d want max(180, 210)=210", sec)
	}
	// Package walls: max(2×wall, wall+120), clipped at 480.
	for _, tc := range []struct{ wall, lease int }{
		{180, 360},
		{240, 480},
		{300, 480},
	} {
		cfg := map[string]any{
			"work_kind":        "hunt_shard",
			"hunt_engine":      "libfuzzer",
			"engine_lease_sec": tc.wall,
			"pool_distributed": true,
		}
		if got := hunt.EngineLeaseSecFromConfig(cfg, "libfuzzer"); got != tc.wall {
			t.Fatalf("wall %d became %d", tc.wall, got)
		}
		if sec := leaseSecondsForConfig(cfg); sec != int64(tc.lease) {
			t.Fatalf("wall %d reclaim=%d want %d", tc.wall, sec, tc.lease)
		}
	}
	t.Setenv("HACKME_HUNT_ENGINE_LEASE_SEC", "")
	unstamped := map[string]any{
		"work_kind":        "hunt_shard",
		"hunt_engine":      "libfuzzer",
		"pool_distributed": true,
	}
	if got := hunt.EngineLeaseSecFromConfig(unstamped, "libfuzzer"); got != 180 {
		t.Fatalf("unstamped wall=%d want 180", got)
	}
	if sec := leaseSecondsForConfig(unstamped); sec != 360 {
		t.Fatalf("unstamped reclaim=%d want 360", sec)
	}
	// Campaign JSON reloads numbers as float64. A stored 90s wall stays 90s.
	jsonStored := map[string]any{
		"work_kind":        "hunt_shard",
		"hunt_engine":      "libfuzzer",
		"hunt_package":     "hunt_heavy",
		"engine_lease_sec": float64(90),
		"pool_distributed": true,
	}
	if got := hunt.EngineLeaseSecFromConfig(jsonStored, "libfuzzer"); got != 90 {
		t.Fatalf("json stored wall=%d want 90", got)
	}
	if sec := leaseSecondsForConfig(jsonStored); sec != 210 {
		t.Fatalf("json stored reclaim=%d want 210", sec)
	}
}

func TestCoverageLeaseBoundaryWalls(t *testing.T) {
	t.Setenv("HACKME_HUNT_COVERAGE_ENGINE", "1")
	t.Setenv("HACKME_HUNT_ENGINE_LEASE_SEC", "")
	// JSON campaign config reloads the wall as float64. 600 and 601 are past
	// MaxEngineLeaseSec, so the stored wall clamps to 300 before the
	// max(2×wall, wall+120) cap of 480. 179 stays 179 (reclaim 358). 299
	// still clamps the reclaim at 480.
	cases := []struct{ wall, wantWall, wantLease int }{
		{179, 179, 358},
		{180, 180, 360},
		{299, 299, 480},
		{300, 300, 480},
		{600, hunt.MaxEngineLeaseSec, 480},
		{601, hunt.MaxEngineLeaseSec, 480},
	}
	for _, tc := range cases {
		cfg := map[string]any{
			"work_kind":        "hunt_shard",
			"hunt_engine":      "libfuzzer",
			"engine_lease_sec": float64(tc.wall),
			"pool_distributed": true,
		}
		if got := hunt.EngineLeaseSecFromConfig(cfg, "libfuzzer"); got != tc.wantWall {
			t.Fatalf("wall %d stored as %d want %d", tc.wall, got, tc.wantWall)
		}
		if sec := leaseSecondsForConfig(cfg); sec != int64(tc.wantLease) {
			t.Fatalf("wall %d reclaim=%d want %d", tc.wall, sec, tc.wantLease)
		}
	}
}
