# Hunt economics — 50/50 split (spec)

**Status:** wired in code on `feature/hunt-mvp` (Dig/Scan still 20/80)  
**Updated:** 2026-08-31  
**Related:** [FUZZ_ESCROW_20_80.md](FUZZ_ESCROW_20_80.md) · [ORDER_ECONOMICS.md](ORDER_ECONOMICS.md) · [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md)

---

## Why Hunt ≠ Dig split

| | Dig / Scan (20/80) | **Hunt (50/50)** |
|--|---------------------|------------------|
| Typical outcome | guard signals common | **CLEAN often** — new ASAN crash not guaranteed |
| Work unit | WASM segment / run | **CPU shard** (heavier) |
| Customer buys | audit + finding lane | **distributed ASAN compute** + crash-if-lucky |
| Miner income if CLEAN | 20% runs pool | **50% runs pool** — still worth taking |
| Refund on CLEAN | up to 80% bounty | up to **50%** bounty (other 50% paid for work) |

**Dig prices unchanged.** Hunt uses its own split when `campaign_type=hunt` (or `depth_tier` hunt-native).

---

## Split (canonical)

| Pool | Share | Pays when |
|------|-------|-----------|
| **Runs / shards** | **50%** | Each verified Hunt shard submit (`PayFuzzRun` / shard settle) |
| **Bounty** | **50%** | First **crash-class** finding with **native repro + challenge pass** |
| **Crash bonus** | **≤1% of bounty pool**, cap **0.05 HMC** | First **unique crash-class** input (`PayFuzzCrashBonus`) — does **not** close main bounty |

Unused bounty (no qualifying finding) **refunds** to payer on finalize — same as Dig.  
Crash bonus already paid is not refunded.

Platform fee: **5%** taken from **bounty payout only** (not from runs pool).

---

## Crash bonus (Hunt)

Formula (Hunt campaigns only):

```
crash_bonus = min(0.01 × bounty_pool_hmc, 0.05 HMC)
```

- Paid **once** per campaign to the miner who submitted the first **unique crash-class** finding.  
- Deducted from the **bounty pool**; main bounty (50% slice) remains unlockable on full qualifying finding.  
- **0** for detector noise, harness_runtime, UBSan/LSan informational (`sanitizer_informational` finding type — see report hygiene appendix with subtypes).  
- **Dig/Scan** keep existing cap **0.01 HMC** until explicitly migrated.

**Examples (50/50):**

| Package | Budget | Bounty pool (50%) | 1% | **Crash bonus paid** |
|---------|--------|-------------------|-----|----------------------|
| Hunt Lite | 20 HMC | 10 HMC | 0.10 | **0.05 HMC** (cap) |
| Hunt Standard | 60 HMC | 30 HMC | 0.30 | **0.05 HMC** (cap) |
| Hunt Heavy | 150 HMC | 75 HMC | 0.75 | **0.05 HMC** (cap) |

---

## Packages (initial)

| Tier | Budget HMC | Shards (target) | Exec/shard | Overnight local | Wall | Min / shard | Notes |
|------|------------|-----------------|------------|-----------------|------|-------------|-------|
| **Hunt Lite** | **20** | **1200** | **32** | **20k iter · 1h** | 6–24h | **≥ 0.002** | MVP — catalog + inventory |
| **Hunt Standard** | **60** | **4000** | **128** | **200k iter · 8h** | 1–3d | **≥ 0.003** | + template harness after Accept |
| **Hunt Heavy** | **150+** | **12000** | **256** | **500k iter · 12h** | 3d+ | dynamic | Pool-scale depth |

**Minimum campaign budget (Hunt):** **15 HMC** (Lite floor in UI), **50 HMC** (Standard).

Compare Dig: Scan **1** · Audit **5** · Deep **25** HMC ([b2b_packages.go](../internal/fuzzingcli/b2b_packages.go)).

---

## Bounty gates (Hunt only)

Main bounty (50% pool) unlock requires **all**:

1. Finding class: **native_crash** / ASAN security sanitizer (heap overflow, UAF, double-free, etc.)  
2. **Repro** confirmed (coordinator replay / challenge)  
3. Severity ≥ **High** for MVP (Critical = full bounty; Medium = hold / partial — Phase 2)

**No bounty** (runs + crash bonus rules only):

- UBSan informational  
- detector / property / guard signals  
- harness_runtime noise  
- CLEAN budget exhausted  

---

## Severity → bounty share (50% pool)

| Severity | Signal | Share of bounty pool |
|----------|--------|----------------------|
| **Critical** | ASAN heap/UAF/double-free + repro | **100%** (− 5% platform fee) |
| **High** | ASAN stack / confirmable crash + repro | **60%** (Phase 2; MVP can treat as Critical) |
| **Medium** | sanitizer, needs triage | **0%** auto — hold for operator |
| **Low / noise** | UBSan info, detectors | **0%** |

First **qualifying** finding wins the bounty slice (same idempotency model as Dig).

---

## PoH attach (optional)

Default Hunt: **fuzz escrow only** (`create_poh_order: false`).

Optional hybrid: `create_poh_order: true`, `reward_hmc: 0.02–0.05` — for rigs running PoH + Hunt shards. Not required for MVP.

---

## Customer copy (pre-pay)

```
Hunt Lite · 20 HMC · 50/50 split
· ~10 HMC → miners for verified shard work (as campaign runs)
· ~10 HMC → bounty if ASAN crash + repro confirmed; else refunded on finalize
· Up to 0.05 HMC crash bonus for first unique crash-class submit
· CLEAN = budget exhausted, not "fully secure"
· No CVE guarantee
```

---

## Miner copy (marketplace)

- Badge **HUNT** · **per_shard_hmc** · live ETA  
- Hunt should target **≥2× Dig Audit** effective HMC/hour on same CPU when pool is healthy  
- Prefer Hunt jobs when per_shard ≥ Dig per_run

---

## Implementation checklist (code)

- [x] `fuzzescrow.ComputeHuntSplitUnits` — 50/50 + Hunt min shard units  
- [x] `campaign_type: hunt` in create API (`POST /api/hunt/campaigns`)  
- [x] `UniqueCrashBonusMaxUnits` override for Hunt: **5_000_000** (0.05 HMC) via `escrow_split`  
- [x] Node: `GET /api/hunt/targets`, `POST /api/hunt/inventory`, `POST /api/hunt/campaigns`  
- [x] Coordinator: CPU shard work kind `hunt_shard` — claim/submit + **coordinator ASAN replay** (`evalHuntSubmitCheck`)  
- [x] Worker: `RunHuntShard` ASAN on catalog harness (`hunt.ReplayShard` + `.cache/hunt-harness/`)  
- **L1 mutating shards:** anchor at claim + `iterations_per_shard` (Lite 32 · Standard 128 · Heavy 256) (`hunt.ShardSegmentExecInput`); coordinator replays full chain; fake-crash reject unchanged  
- **L2 corpus-guided:** `hunt_corpus_guided` + frozen `corpus_seeds` at claim; pool corpus bootstrap/observe; namespace persist `hunt:{target_id}`  
- **Overnight local:** `hunt_local_runner` + autorunner ticks (`hunt.LocalAutorunTick`) — hours-scale without pool rewrite  
- **Domain mutator dict:** `ApplyHuntMutatorDict` per catalog target (JSON/XML/INI/TOML/msgpack)  
	- [x] **Sanitizer profile:** default `asan+ubsan+lsan` (`hunt_detect_leaks: true`); UBSan/LSan → `sanitizer_informational` hygiene appendix (not bounty)  
- [x] UI: Hunt card + **pre-pay scope block** `#hunt-scope-contract`  
- [x] Gate: `scripts/tests/hunt_pool_smoke_gate.sh` (fake crash reject + worker smoke)  
- [x] Benchmark: `scripts/tests/hunt_standard128_live_benchmark.sh` + [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md)  
- [x] Inventory C/C++: multi-file `clang++` harness, companion sources, CMake `build_hints` — `scripts/tests/hunt_inventory_cpp_gate.sh`
- [x] Inventory Rust Phase A: `.rs` detect + catalog `language=rust` ASAN (`serde_json`, **`memchr`**, **`quick_xml`**) via cargo nightly — `scripts/tests/hunt_inventory_rust_gate.sh` · [HUNT_RUST_PHASE_A.md](HUNT_RUST_PHASE_A.md)
- [x] Hunt gates (also): `hunt_report_gate.sh`, `hunt_harness_publish_gate.sh`, `hunt_mutator_trim_gate.sh`, `hunt_l2_seeds_ab_gate.sh`, `hunt_corpus_import_gate.sh`, `hunt_async_swarm_gate.sh`, `hunt_replay_async_gate.sh`
- [x] Customer repo pin → harness build (`POST /api/hunt/repo/pin`, `/harness/build`, `/template/preview`)  
- [x] Harness publish API + coordinator worker fetch (`/harness/publish`, `/api/fuzz/pool/hunt/harness/{hash}`)  
- [x] CLI: `hackme-fuzzing hunt pin|inventory|build|create`  
- [x] Dashboard: inventory mode + template Accept  
- [x] Docs cross-link from [FUZZ_ESCROW_20_80.md](FUZZ_ESCROW_20_80.md) · HTTP tables in [API.md](API.md)#hunt-campaigns-phase-2

---

## Honest limits

- 50/50 does **not** guarantee miners profit if pool is tiny or ETA is weeks — cap budgets + live ETA.  
- 50/50 does **not** promise CVE — only fair pay for compute + crash jackpot lane.  
- Hunt is **not** positioned as a libFuzzer replacement — see [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md) (libFuzzer wins exec/s; Hunt wins fleet + deliverable).  
- Foreign CEX price of HMC does not change these **in-ecosystem** unit rules.
