# Hunt vs libFuzzer — live product comparison (updated)

**Status (2026-10-11):** product Hunt is **coverage-guided at fleet scale** — **libFuzzer** default · **AFL++** Deep when engines are installed.  
**Live re-bench:** `reports/hunt-benchmark/coverage-20261011T130004Z/` · re-run: `bash scripts/tests/hunt_coverage_engine_benchmark.sh`  
**Related:** [HUNT_COVERAGE_ENGINES.md](HUNT_COVERAGE_ENGINES.md) · [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md) · [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md)

---

## Positioning (read this first)

| | **Standalone libFuzzer / OSS-Fuzz research** | **HackMe Hunt (product)** |
|--|---------------------------------------------|---------------------------|
| **What it is** | Best-in-class local coverage engine | **Same engine class** (LF + AFL++) on a **paid pool** with prove + escrow |
| **Unit of work** | In-process exec + local corpus | Engine **lease wall** + `corpus_delta` + coordinator **ASAN + afl-showmap** |
| **Who wins exec/s** | Local machine, no network | Not the KPI — we ship **verified distributed depth** |
| **Who wins the job** | Maintainer overnight on one box | Customer / miner fleet: **report, edges proven, escrow, Dig hybrid earn** |

**Sell hard:** *coverage-guided Hunt is live on the network — libFuzzer and AFL++ are first-class engines, paid on proven edges. Engine auto-install ships on published **rc18** installers.*  
**Do not sell:** “faster than libFuzzer” or “better than OSS-Fuzz.” Sell **fleet + prove + escrow + hybrid Dig**.

Dig stays **WASM-only**. LF/AFL never enter Dig.

---

## Live benchmark (90s wall, same machine) — 2026-10-11

**Stamp:** `coverage-20261011T130004Z`  
**Command:** `WALL_SEC=90 bash scripts/tests/hunt_coverage_engine_benchmark.sh`

| Target | Lane | Metric | Result |
|--------|------|--------|--------|
| **cjson** | Standalone libFuzzer | exec/s | **15,883** (1.45M execs) |
| **cjson** | **Hunt libFuzzer engine** | corpus Δ | **+85** (335→420) in ~93s lease |
| **cjson** | Hunt AFL++ engine | corpus Δ | +0 (short window / harness setup — Deep path present) |
| **cjson** | byte-Hunt (legacy) | exec/s | **29.4** |
| **libucl** | Standalone libFuzzer | exec/s | **5,595** (509k execs) |
| **libucl** | **Hunt libFuzzer engine** | corpus Δ | **+20** seeds in lease |
| **libucl** | byte-Hunt (legacy) | exec/s | **22.8** |

### What this proves

1. **Product Hunt is no longer the old byte mutator race.** The fleet default is a real **libFuzzer lease** that grows corpus under the same sanitizer class customers care about.
2. **Standalone LF still prints the biggest exec/s number** — that is expected and we do not compete on that chart.
3. **Hunt’s KPI is corpus Δ + coordinator-proven edges/crashes**, not raw local exec/s.
4. The old “~113 vs ~700 exec/s” story was **byte-Hunt vs LF** (see archive table below). **That comparison is obsolete for marketing the current product.**

### How to talk about speed in public

- ✅ “Hunt runs **coverage-guided libFuzzer / AFL++** on the pool — same engine generation as the research world.”
- ✅ “Miners install clang + AFL++ + bubblewrap (`install_hunt_engines.sh`; on **rc18**); Dig keeps earning while Hunt leases run.”
- ✅ “Pay for **proven new edges** and/or **ASAN crashes** — not worker self-reported counters.”
- ❌ “We beat libFuzzer at 15k exec/s.”
- ❌ “We replaced OSS-Fuzz.”

---

## Historical archive — byte path (2026-08-31)

Pre-coverage-engine **byte / L1 shard** numbers. Kept for honesty only.

**Artifacts:** `reports/hunt-benchmark/20260831T162653Z/`  
**Re-run (legacy):** `bash scripts/tests/hunt_standard128_live_benchmark.sh`

| Target | Engine | Executions | exec/s | Outcome |
|--------|--------|------------|--------|---------|
| **cjson** | Hunt local (Standard, byte) | 11,976 | 133 | CLEAN |
| **cjson** | libFuzzer | 70,811 | 778 | clean session |
| **cjson** | Hunt pool | 384 (3×128) | — | coordinator-verified shards |
| **libucl** | Hunt local | 6,857 | 76 | INFORMATIONAL (UBSan hygiene) |
| **libucl** | libFuzzer | 16,641 | 182 | 0 crash artifacts (90s) |

Those ~5–6× exec/s gaps were **expected for byte vs in-process LF**. Current product closed the *engine-class* gap by **adopting LF/AFL++**, not by racing byte mutators.

---

## Customer / sales copy (strong)

**Say:**

- “HackMe Hunt is now **distributed coverage-guided fuzzing** — **libFuzzer** on the fleet, **AFL++** on Deep — with **ASAN + showmap prove** on the coordinator.”
- “We did not invent a toy fuzzer to look busy. We **wired production engines into a miner network** with escrow and hybrid Dig.”
- “Update the miner binary → engines install **automatically** (clang, AFL++, bubblewrap). Soft-skip if missing; Dig never breaks.”
- “Pay model: **proven edges / ASAN crash = full PayRun**; CLEAN without map delta = floor. No trust in worker `edges_touched`.”

**Do not say:**

- “Better / faster than OSS-Fuzz.”
- “113 vs 700 is our current speed.”
- “65 CVEs” from hygiene runs.
- “Memory bugs guaranteed on every campaign.”

---

## Operator commands

```bash
# Current product benchmark (coverage engines)
bash scripts/tests/hunt_coverage_engine_benchmark.sh

# Install engines (also auto on miner install/update)
bash scripts/ops/install_hunt_engines.sh --desktop
sudo bash scripts/ops/install_hunt_engines.sh --system --prefix /opt/hackme

# Legacy byte vs LF (archive)
bash scripts/tests/hunt_standard128_live_benchmark.sh
```

---

## Dig · Hunt · research lanes

| Lane | Model | Honesty |
|------|-------|---------|
| **Dig (pool)** | WASM + own mutator; seed-from-research OFF | Earn floor for miners |
| **Hunt (pool)** | LF/AFL++ leases + prove | Product coverage depth |
| **OSS libFuzzer overnight** | Local research (`run_oss_libfuzzer_session.sh`) | Separate from customer deliverables |

---

## Verdict

Hunt **leveled up to the real coverage stack**. Raw local exec/s still belongs to standalone libFuzzer; **the product win is the network** — pinned engines, auto-install miners, coordinator prove, escrow reports, Dig hybrid. That is the story to push — not the old byte exec/s chart.
