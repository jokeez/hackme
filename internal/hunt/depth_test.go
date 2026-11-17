package hunt

import "testing"

func TestIterationsPerShardForPackage(t *testing.T) {
	if got := IterationsPerShardForPackage("hunt_lite"); got != 64 {
		t.Fatalf("lite=%d", got)
	}
	if got := IterationsPerShardForPackage("hunt_standard"); got != 224 {
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
	if cfg["iterations_per_shard"] != 224 {
		t.Fatalf("iter=%v", cfg["iterations_per_shard"])
	}
	if cfg["hunt_overnight_local"] == true {
		t.Fatal("pool should not set overnight local")
	}
}

func TestPoolEngineLeaseDefaultsByPackage(t *testing.T) {
	mk := func(pkg string) map[string]any {
		cfg := map[string]any{
			"depth_tier": "oss_cve", "escrow_split": "50_50", "hunt_package": pkg,
			"upstream_target": "oss", "upstream_target_id": "jsmn", "pool_distributed": true,
		}
		ApplyPoolGuidedDefaults(cfg, "jsmn")
		ApplyHuntPowerScheduling(cfg, pkg)
		ApplyPackageDepthDefaults(cfg, pkg, true)
		// Mirror config.go pool lease block
		if _, ok := cfg["engine_lease_sec"]; !ok {
			switch pkg {
			case "hunt_heavy":
				cfg["engine_lease_sec"] = DefaultAFLPPLeaseSec
			case "hunt_standard":
				cfg["engine_lease_sec"] = 150
			default:
				cfg["engine_lease_sec"] = 90
			}
		}
		return cfg
	}
	if got := mk("hunt_lite")["engine_lease_sec"]; got != 90 {
		t.Fatalf("lite lease=%v", got)
	}
	if got := mk("hunt_standard")["engine_lease_sec"]; got != 150 {
		t.Fatalf("standard lease=%v", got)
	}
	if got := mk("hunt_heavy")["engine_lease_sec"]; got != DefaultAFLPPLeaseSec {
		t.Fatalf("heavy lease=%v", got)
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

func TestDepthSummaryForConfig(t *testing.T) {
	cfg := map[string]any{"hunt_package": "hunt_standard", "pool_distributed": true, "iterations_per_shard": 128}
	s := DepthSummaryForConfig(cfg)
	if s["iterations_per_shard"] != 128 || s["hunt_package"] != "hunt_standard" {
		t.Fatalf("summary=%v", s)
	}
}

func TestLocalRunBudgetFromConfig(t *testing.T) {
	cfg := map[string]any{"hunt_local_budget_iterations": 99_000}
	if got := LocalRunBudgetFromConfig(cfg, "hunt_lite"); got != 99_000 {
		t.Fatalf("got=%d", got)
	}
}
