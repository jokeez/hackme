package hunt

import "hackme/internal/fuzzescrow"

// Packages returns Hunt Lite / Standard presets (HUNT_ECONOMICS.md).
func Packages() []PackageInfo {
	return []PackageInfo{
		{
			Key:          "hunt_lite",
			Title:        "Hunt Lite",
			BudgetHMC:    20,
			BudgetShards: 1200,
			MinPerShard:  0.002,
			WallHours:    "6–24h",
			Summary:      "Reuse existing upstream fuzz target · 50/50 escrow · MVP",
			EscrowSplit:  fuzzescrow.EscrowSplit5050,
		},
		{
			Key:          "hunt_standard",
			Title:        "Hunt Standard",
			BudgetHMC:    60,
			BudgetShards: 4000,
			MinPerShard:  0.003,
			WallHours:    "1–3d",
			Summary:      "Larger shard budget · template Accept in Phase 2",
			EscrowSplit:  fuzzescrow.EscrowSplit5050,
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
