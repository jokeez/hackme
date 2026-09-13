# Hunt engine — v2.5

**Engine:** `fuzz_engine_v2.5`

## Focus (not WASM Dig)

Hunt CVE lane: honest reports + smarter corpus cull. Dig/WASM stays as-is.

## What landed

| Feature | Why |
|---------|-----|
| **`finding_families` in report** | Roll 65 inputs → N root-cause families (libucl-style honesty) |
| **HTML family card** | Customer-visible: cite `family_count`, not raw crashes |
| **Top issue `finding_family`** | Each crash row stamped with family key + member count |
| **Rarity-aware corpus cull** | Keep crash + rare-edge + hot energy seeds when over `pool_corpus_max` |
| **`CorpusHealthSnapshot`** | Fleet waste / rare / diversity stats for Hunt ops |

## Sales / report honesty

```
Say:  "2 finding families (65 variant inputs)"
Not:  "65 vulnerabilities"
```

## Bench

```bash
go test . -run 'FindingFamily|CollapseCrash|RenderFuzzFamily' -count=1
go test ./internal/fuzzengine/ ./internal/poolfuzz/ -count=1 -timeout 3m
bash scripts/tests/hunt_engine_depth_bench.sh
```
