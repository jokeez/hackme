package poolfuzz

import (
	"testing"

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
	wantMin := int64((64*int(timeoutMS))/1000 + 60)
	if sec < wantMin {
		t.Fatalf("lease %d too short for 64 exec (want >= %d)", sec, wantMin)
	}
	if sec > 600 {
		t.Fatal("lease must be capped at 600s")
	}
}

func TestLeaseSecondsForHuntShardUsesIterations(t *testing.T) {
	cfg := map[string]any{
		"work_kind":            "hunt_shard",
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
	cfg := map[string]any{
		"work_kind":            "hunt_shard",
		"iterations_per_shard": 256,
		"pool_distributed":     true,
	}
	if sec := leaseSecondsForConfig(cfg); sec != 360 {
		t.Fatalf("heavy hunt lease=%d want 360", sec)
	}
}
