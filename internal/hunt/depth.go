package hunt

import "strings"

const (
	huntIterPerShardLite     = 32
	huntIterPerShardStandard = 128
	huntIterPerShardHeavy    = 256
	maxShardIterationsPer    = 256

	huntLocalIterLite     = 20_000
	huntLocalIterStandard = 200_000
	huntLocalIterHeavy    = 500_000

	huntLocalSecLite     = 3_600    // 1h
	huntLocalSecStandard = 28_800   // 8h
	huntLocalSecHeavy    = 43_200   // 12h

	huntLocalTickIter = 2_000
)

// IterationsPerShardForPackage returns pool shard depth for a Hunt SKU.
func IterationsPerShardForPackage(pkgKey string) int {
	switch strings.TrimSpace(strings.ToLower(pkgKey)) {
	case "hunt_standard", "standard":
		return huntIterPerShardStandard
	case "hunt_heavy", "heavy":
		return huntIterPerShardHeavy
	default:
		return huntIterPerShardLite
	}
}

// LocalOvernightBudget returns default iteration budget for overnight local Hunt.
func LocalOvernightBudget(pkgKey string) int {
	switch strings.TrimSpace(strings.ToLower(pkgKey)) {
	case "hunt_standard", "standard":
		return huntLocalIterStandard
	case "hunt_heavy", "heavy":
		return huntLocalIterHeavy
	default:
		return huntLocalIterLite
	}
}

// LocalOvernightTimeLimitSec returns default wall budget for overnight local Hunt.
func LocalOvernightTimeLimitSec(pkgKey string) int {
	switch strings.TrimSpace(strings.ToLower(pkgKey)) {
	case "hunt_standard", "standard":
		return huntLocalSecStandard
	case "hunt_heavy", "heavy":
		return huntLocalSecHeavy
	default:
		return huntLocalSecLite
	}
}

// ApplyPackageDepthDefaults sets iterations_per_shard and optional overnight local budgets.
func ApplyPackageDepthDefaults(cfg map[string]any, pkgKey string, poolDistributed bool) {
	if cfg == nil {
		return
	}
	if _, ok := cfg["iterations_per_shard"]; !ok {
		if poolDistributed {
			cfg["iterations_per_shard"] = IterationsPerShardForPackage(pkgKey)
		}
	}
	if !poolDistributed {
		cfg["hunt_overnight_local"] = true
		if _, ok := cfg["hunt_local_budget_iterations"]; !ok {
			cfg["hunt_local_budget_iterations"] = LocalOvernightBudget(pkgKey)
		}
		if _, ok := cfg["hunt_local_time_limit_sec"]; !ok {
			cfg["hunt_local_time_limit_sec"] = LocalOvernightTimeLimitSec(pkgKey)
		}
		if _, ok := cfg["hunt_local_tick_iterations"]; !ok {
			cfg["hunt_local_tick_iterations"] = huntLocalTickIter
		}
	}
}

// PackageKeyFromConfig reads hunt_package from campaign config.
func PackageKeyFromConfig(cfg map[string]any) string {
	if cfg == nil {
		return "hunt_lite"
	}
	if v := strings.TrimSpace(cfgString(cfg, "hunt_package")); v != "" {
		return v
	}
	return "hunt_lite"
}

// LocalRunBudgetFromConfig resolves iteration budget for a local/overnight Hunt run.
func LocalRunBudgetFromConfig(cfg map[string]any, pkgKey string) int {
	if cfg != nil {
		if n := cfgInt(cfg, "hunt_local_budget_iterations"); n > 0 {
			return n
		}
		if n := cfgInt(cfg, "budget_iterations"); n > 0 {
			return n
		}
	}
	return LocalOvernightBudget(pkgKey)
}

// LocalRunTimeLimitFromConfig resolves wall budget seconds for local Hunt.
func LocalRunTimeLimitFromConfig(cfg map[string]any, pkgKey string) int {
	if cfg != nil {
		if n := cfgInt(cfg, "hunt_local_time_limit_sec"); n > 0 {
			return n
		}
		if n := cfgInt(cfg, "time_limit_sec"); n > 0 {
			return n
		}
	}
	return LocalOvernightTimeLimitSec(pkgKey)
}
