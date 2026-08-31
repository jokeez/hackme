package hunt

import "testing"

func TestIterationsPerShardForPackage(t *testing.T) {
	if got := IterationsPerShardForPackage("hunt_lite"); got != 32 {
		t.Fatalf("lite=%d", got)
	}
	if got := IterationsPerShardForPackage("hunt_standard"); got != 128 {
		t.Fatalf("standard=%d", got)
	}
	if got := IterationsPerShardForPackage("hunt_heavy"); got != 256 {
		t.Fatalf("heavy=%d", got)
	}
}

func TestShardIterationsPerCap256(t *testing.T) {
	cfg := map[string]any{"iterations_per_shard": 512}
	if got := ShardIterationsPer(cfg); got != 256 {
		t.Fatalf("cap got=%d", got)
	}
}

func TestApplyPackageDepthDefaultsPool(t *testing.T) {
	cfg := map[string]any{}
	ApplyPackageDepthDefaults(cfg, "hunt_standard", true)
	if cfg["iterations_per_shard"] != 128 {
		t.Fatalf("iter=%v", cfg["iterations_per_shard"])
	}
	if cfg["hunt_overnight_local"] == true {
		t.Fatal("pool should not set overnight local")
	}
}

func TestApplyPackageDepthDefaultsLocal(t *testing.T) {
	cfg := map[string]any{}
	ApplyPackageDepthDefaults(cfg, "hunt_standard", false)
	if cfg["hunt_overnight_local"] != true {
		t.Fatalf("overnight=%v", cfg["hunt_overnight_local"])
	}
	if cfg["hunt_local_budget_iterations"] != huntLocalIterStandard {
		t.Fatalf("budget=%v", cfg["hunt_local_budget_iterations"])
	}
}

func TestLocalRunBudgetFromConfig(t *testing.T) {
	cfg := map[string]any{"hunt_local_budget_iterations": 99_000}
	if got := LocalRunBudgetFromConfig(cfg, "hunt_lite"); got != 99_000 {
		t.Fatalf("got=%d", got)
	}
}
