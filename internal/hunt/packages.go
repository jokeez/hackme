package hunt

import "hackme/internal/fuzzescrow"

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
			Summary:            "Smoke Hunt · pool ASAN shards · attested CLEAN ≠ CVE · not OSS-Fuzz replacement",
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
			WallHours:          "1–3d",
			Summary:            "Smoke Hunt · deeper pool sweep · attested CLEAN ≠ CVE · not OSS-Fuzz replacement",
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
			WallHours:          "3d+",
			Summary:            "Deep Hunt · pool-scale depth · stricter budgets · CLEAN ≠ CVE · not OSS-Fuzz replacement",
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
