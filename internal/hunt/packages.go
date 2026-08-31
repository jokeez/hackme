package hunt

import "hackme/internal/fuzzescrow"

// Packages returns Hunt Lite / Standard presets (HUNT_ECONOMICS.md).
func Packages() []PackageInfo {
	return []PackageInfo{
		{
			Key:                 "hunt_lite",
			Title:               "Hunt Lite",
			BudgetHMC:           20,
			BudgetShards:        1200,
			IterationsPerShard:  huntIterPerShardLite,
			LocalBudgetIters:    huntLocalIterLite,
			LocalTimeLimitSec:   huntLocalSecLite,
			MinPerShard:         0.002,
			WallHours:           "6–24h",
			Summary:             "Pool sweep · 32 exec/shard · overnight local up to 20k iter",
			EscrowSplit:         fuzzescrow.EscrowSplit5050,
		},
		{
			Key:                 "hunt_standard",
			Title:               "Hunt Standard",
			BudgetHMC:           60,
			BudgetShards:        4000,
			IterationsPerShard:  huntIterPerShardStandard,
			LocalBudgetIters:    huntLocalIterStandard,
			LocalTimeLimitSec:   huntLocalSecStandard,
			MinPerShard:         0.003,
			WallHours:           "1–3d",
			Summary:             "Pool sweep · 128 exec/shard · overnight local up to 200k iter",
			EscrowSplit:         fuzzescrow.EscrowSplit5050,
		},
		{
			Key:                 "hunt_heavy",
			Title:               "Hunt Heavy",
			BudgetHMC:           150,
			BudgetShards:        12000,
			IterationsPerShard:  huntIterPerShardHeavy,
			LocalBudgetIters:    huntLocalIterHeavy,
			LocalTimeLimitSec:   huntLocalSecHeavy,
			MinPerShard:         0.003,
			WallHours:           "3d+",
			Summary:             "Pool-scale · 256 exec/shard · overnight local up to 500k iter",
			EscrowSplit:         fuzzescrow.EscrowSplit5050,
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
