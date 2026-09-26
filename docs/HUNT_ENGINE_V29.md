# Hunt / Dig engine — v2.9

**Engine:** `fuzz_engine_v2.9`

## vs v2.8 / v2.7

| | v2.7 | v2.8 | v2.9 |
|--|------|------|------|
| Havoc ops | 64 | 80 | **80 + soft salt-keyed weight table** (CmpLog/splice/dict bias) |
| Stack depth | ≤32 | ≤36 | ≤36 (Heavy `power_mut_cap` ≥16 → deeper guided stages) |
| Pre-havoc crossover | every 7th | every 4th | **every 3rd** + **two-point/ordered splice** |
| Power / rarity | edge | stronger edge + cull | **+ path-hit rarity + mid-len length-class energy** |
| Observe energy | crash flat | crash + novelty | **hang/timeout boost separate from crash** |
| Hunt Heavy cap | 12 | 12 | **16** (disable with `hunt_heavy_power_boost=false`) |
| Domain dict | baseline | baseline | **expanded JSON/XML/INI/TOML/msgpack tokens** |

## Frozen T0 (do not regress)

v2.8 @5k havoc grid: **unique≈4987 lens≈250**.

Measured v2.9 @5k: **unique=4986 lens=250** (−1 unique / +0 lens vs T0; within ≤40 unique jitter; lens holds). Gain vs upstream baseline: **+4.1% / +363%**.

## Honesty

Deterministic stage+salt only — no online MOpt. Preserves NUL `CompactCorpusSeed`, family dedupe, GHS coupling, pool replay.

## Local stress

```bash
go test ./internal/fuzzengine/ ./internal/hunt/ ./internal/workerfuzzloop/ -count=1
bash scripts/tests/fuzz_engine_local_stress.sh
```
