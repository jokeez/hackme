package hunt

import (
	"strconv"
	"strings"
	"testing"
)

func TestIterationsPerShardForPackage(t *testing.T) {
	if got := IterationsPerShardForPackage("hunt_lite"); got != 96 {
		t.Fatalf("lite=%d", got)
	}
	if got := IterationsPerShardForPackage("hunt_standard"); got != 288 {
		t.Fatalf("standard=%d", got)
	}
	if got := IterationsPerShardForPackage("hunt_heavy"); got != 384 {
		t.Fatalf("heavy=%d", got)
	}
}

func TestShardIterationsPerCap384(t *testing.T) {
	cfg := map[string]any{"iterations_per_shard": 512}
	if got := ShardIterationsPer(cfg); got != 384 {
		t.Fatalf("cap got=%d", got)
	}
}

func TestApplyPackageDepthDefaultsPool(t *testing.T) {
	cfg := map[string]any{}
	ApplyPackageDepthDefaults(cfg, "hunt_standard", true)
	if cfg["iterations_per_shard"] != 288 {
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
		if _, ok := cfg["engine_lease_sec"]; !ok {
			cfg["engine_lease_sec"] = PoolEngineLeaseSecForPackage(pkg)
		}
		return cfg
	}
	if got := mk("hunt_lite")["engine_lease_sec"]; got != PoolEngineLeaseSecLite {
		t.Fatalf("lite lease=%v want %d", got, PoolEngineLeaseSecLite)
	}
	if got := mk("hunt_standard")["engine_lease_sec"]; got != PoolEngineLeaseSecStandard {
		t.Fatalf("standard lease=%v want %d", got, PoolEngineLeaseSecStandard)
	}
	if got := mk("hunt_heavy")["engine_lease_sec"]; got != PoolEngineLeaseSecHeavy {
		t.Fatalf("heavy lease=%v want %d", got, PoolEngineLeaseSecHeavy)
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

func TestPackageSummariesMatchLeaseConstants(t *testing.T) {
	for _, p := range Packages() {
		want := strconv.Itoa(PoolEngineLeaseSecForPackage(p.Key)) + "s"
		if !strings.Contains(p.Summary, want) {
			t.Fatalf("%s summary %q missing %s", p.Key, p.Summary, want)
		}
	}
	std := PackageByKey("hunt_standard")
	heavy := PackageByKey("hunt_heavy")
	if std == nil || strings.Contains(std.Summary, "180s") {
		t.Fatalf("standard still advertises 180s: %+v", std)
	}
	if heavy == nil || strings.Contains(heavy.Summary, "210s") {
		t.Fatalf("heavy still advertises 210s: %+v", heavy)
	}
}

func TestLocalRunBudgetFromConfig(t *testing.T) {
	cfg := map[string]any{"hunt_local_budget_iterations": 99_000}
	if got := LocalRunBudgetFromConfig(cfg, "hunt_lite"); got != 99_000 {
		t.Fatalf("got=%d", got)
	}
}
