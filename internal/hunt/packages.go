package hunt

import (
	"fmt"

	"hackme/internal/fuzzescrow"
)

// Packages returns Hunt Lite / Standard presets (HUNT_ECONOMICS.md).
func Packages() []PackageInfo {
	return []PackageInfo{
		{
			Key:                "hunt_lite",
			Title:              "Hunt Lite",
			BudgetHMC:          20,
			BudgetShards:       1200,
			IterationsPerShard: huntIterPerShardLite,
			LocalBudgetIters:   huntLocalIterLite,
			LocalTimeLimitSec:  huntLocalSecLite,
			MinPerShard:        0.002,
			WallHours:          "6–24h",
			Summary:            fmt.Sprintf("Pool ASAN Lite · %ds leases · 96 iters/shard · profile dict · attested CLEAN ≠ CVE", PoolEngineLeaseSecLite),
			EscrowSplit:        fuzzescrow.EscrowSplit5050,
		},
		{
			Key:                "hunt_standard",
			Title:              "Hunt Standard",
			BudgetHMC:          60,
			BudgetShards:       4000,
			IterationsPerShard: huntIterPerShardStandard,
			LocalBudgetIters:   huntLocalIterStandard,
			LocalTimeLimitSec:  huntLocalSecStandard,
			MinPerShard:        0.003,
			WallHours:          "6–16h on the pool (measured Nov 2026)",
			Summary:            fmt.Sprintf("Pool ASAN Standard · %ds leases · 288 iters/shard · v210 havoc · guided corpus · CLEAN ≠ CVE", PoolEngineLeaseSecStandard),
			EscrowSplit:        fuzzescrow.EscrowSplit5050,
		},
		{
			Key:                "hunt_heavy",
			Title:              "Hunt Heavy",
			BudgetHMC:          150,
			BudgetShards:       12000,
			IterationsPerShard: huntIterPerShardHeavy,
			LocalBudgetIters:   huntLocalIterHeavy,
			LocalTimeLimitSec:  huntLocalSecHeavy,
			MinPerShard:        0.003,
			WallHours:          "~1 day on the pool (12k units)",
			Summary:            fmt.Sprintf("Pool ASAN Heavy · %ds leases · max shard depth · CLEAN ≠ CVE", PoolEngineLeaseSecHeavy),
			EscrowSplit:        fuzzescrow.EscrowSplit5050,
		},
	}
}

// PackageByKey returns preset or nil.
func PackageByKey(key string) *PackageInfo {
	for _, p := range Packages() {
		if p.Key == key {
			cp := p
			return &cp
		}
	}
	return nil
}
