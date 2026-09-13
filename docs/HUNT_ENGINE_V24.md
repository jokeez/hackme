# Hunt engine — v2.4

**Engine:** `fuzz_engine_v2.4`

## Audit fixes (critical)

| Bug in v2.3 | Fix in v2.4 |
|-------------|-------------|
| Weighted seeds by **edge bucket ID** (`Edge/10`) as if higher = better | Weight by **edge rarity** (hit counts). Rare edges win. |
| `upsert` used `MAX(energy)` so decay never applied | `energy=excluded.energy` after `ApplyObserveEnergy` |
| Preferred longer corpus bytes on conflict | Prefer **compact** non-empty seeds when shorter |

## New capabilities

| Feature | Effect |
|---------|--------|
| **Power schedule** | Rare + hot seeds get deeper havoc stages |
| **Corpus decay** | Flat observes cool energy → fleet rotates |
| **CompactCorpusSeed** | Clamp maxLen only (preserve trailing NULs for binary seeds) |
| **Diversity metrics** | `MeasureGuidedDiversity` → unique/waste ratio |
| **RankCorpusForCull** | Crash > rare+energy > compact |

## Bench

```bash
bash scripts/tests/hunt_engine_depth_bench.sh
go test ./internal/fuzzengine/ -run 'Power|Decay|Diversity|Rarity|Compact|Rank' -v
```
