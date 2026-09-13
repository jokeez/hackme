# Hunt engine depth — v2.1 (FounderB contribution)

**Branch:** `feat/hunt-engine-depth` (from `feature/hunt-mvp`)  
**Engine:** `fuzz_engine_v2.1`

## What changed

| Area | Change |
|------|--------|
| **Havoc depth** | Stacked havoc (`havocStackDepth`), 24 ops (was 16), BE interesting, arith32, structure smash, length-prefix footguns |
| **Corpus** | Crossover between parents inside `MutateBytesForHunt`; opt-in `corpus_explore_v2` (default **on** for Hunt L2 via `ApplyPoolGuidedDefaults`) so low-energy seeds keep a floor share |
| **Families** | Richer UBSan kinds (`fn_ptr_cast`, `misaligned`, …) + `FindingFamily` / `CountFindingFamilies` for “65 inputs → 1 family” reports |
| **Energy** | Stronger `CorpusObserveBoost` on new edges / findings |
| **Metrics** | `MeasureMutationDepth` + `scripts/tests/hunt_engine_depth_bench.sh` |

## Replay safety

- Deterministic: same `(base, stage, salt, dict, corpus)` → same bytes (pool coordinator replay).
- Classic `PickWeightedSeed` unchanged; explore weights only when `corpus_explore_v2=true`.

## Bench (local unit)

```bash
bash scripts/tests/hunt_engine_depth_bench.sh
```

Example depth sample (500 havoc-heavy mix): **unique_ratio ≈ 0.30**, havoc unique ratio **1.0**.

## Honest limits

This improves **mutation diversity / fleet seed share / report families**.  
It does **not** claim higher ASAN exec/s than libFuzzer on one core — that remains the research lane.
