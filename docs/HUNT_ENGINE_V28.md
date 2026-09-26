# Hunt / Dig engine — v2.8

**Engine:** `fuzz_engine_v2.8`

## vs v2.7

| | v2.7 | v2.8 |
|--|------|------|
| Havoc ops | 64 | **80** (CmpLog-inspired + shape churn) |
| Stack depth | ≤32 | **≤36** |
| Pre-havoc crossover | every 7th | **every 4th** |
| Deterministic stages | bitflip only | **bitflip / arith / interesting / CmpLog** |
| Autodict | first-seen tokens | **frequency-ranked + magic + CmpLog consts** |
| Power / rarity | edge hit boosts | **stronger singleton rare + crash pull** |
| Corpus cull | rank by weight | **CullCorpusKeep: crash + singleton edges first** |
| Coverage boost | bitmap /8 | denser bitmap + dual-novelty |

## Frozen T0 (do not regress)

v2.7 @5k havoc grid: **unique≈4987 lens≈249**.

Measured v2.8 @5k: **unique=4987 lens=250** (+0 unique / +1 lens vs T0; +4.1% / +363% vs upstream baseline).
At 50k: unique gain **+8.1%**, lens gain **+300%** vs upstream.

## Honesty

Mutation uniqueness ≠ CVE. Gains are **input diversity** for ASAN fleet depth. ASAN/WASM stay **CPU**; GHS coupling unchanged.

## Local stress

```bash
go test ./internal/fuzzengine/ ./internal/hunt/ ./internal/workerfuzzloop/ -count=1
bash scripts/tests/fuzz_engine_local_stress.sh
bash scripts/tests/hunt_engine_depth_bench.sh
```
