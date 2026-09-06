# Hunt vs libFuzzer — honest depth comparison

**Status:** live benchmark on `feature/hunt-mvp` (2026-08-31)  
**Related:** [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md) · [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md) · [OSS_CVE_DISCLOSURE_libucl.md](OSS_CVE_DISCLOSURE_libucl.md)

Hunt and libFuzzer solve **different jobs**. This doc records what a **repeatable local benchmark** showed — not marketing claims.

---

## Positioning (read this first)

| | **libFuzzer** (OSS research lane) | **Hunt** (B2B product) |
|--|-----------------------------------|------------------------|
| **Buyer** | Maintainer / security researcher | Customer who wants a **deliverable** |
| **Unit of work** | In-process exec + corpus | Pool shard (verified replay) or overnight local |
| **Depth model** | Coverage-guided mutation | Byte mutation + domain dict + L2 corpus (pool) |
| **Output** | Crash dir, stats, `-print_final_stats` | HTML report, CI gate, escrow, 50/50 economics |
| **Win on** | **exec/s**, corpus feedback | **Fleet verification**, sanitizer hygiene appendix, turnkey |

**Do not sell Hunt as “libFuzzer but better”.**  
Sell: *distributed ASAN compute + verified report + escrow. libFuzzer is your R&D lane; Hunt is your production security run.*

OSS libFuzzer campaigns (`scripts/ops/run_oss_libfuzzer_session.sh`) remain a **separate research lane** — do not conflate with customer Hunt deliverables.

---

## Live benchmark (90s wall-time, same machine)

**Artifacts:** `reports/hunt-benchmark/20260831T162653Z/`  
**Re-run:** `bash scripts/tests/hunt_standard128_live_benchmark.sh`

| Target | Engine | Executions | exec/s | Outcome |
|--------|--------|------------|--------|---------|
| **cjson** | Hunt local (Standard) | 11,976 | 133 | CLEAN |
| **cjson** | libFuzzer | 70,811 | 778 | clean session |
| **cjson** | **Hunt pool** | **384** (3×**128**) | — | 3 shards coordinator-verified |
| **libucl** | Hunt local | 6,857 | 76 | INFORMATIONAL (UBSan hygiene) |
| **libucl** | libFuzzer | 16,641 | 182 | 0 crash artifacts (90s smoke) |

### Pool Standard 128 — proven

```
shards_done=3  iterations_per_shard=128  total_shard_execs=384  hunt_package=hunt_standard
```

Coordinator **replays the full 128-exec chain** per shard. Standard tier depth is real, not brochure copy.

### libucl hygiene — one root cause, many inputs

Hunt reported **65** sanitizer hits in 90s. Follow-up analysis:

- **65 unique byte inputs** (Hunt dedupes by input hex — no exact duplicates)
- **1 dominant UBSan class:** `function-pointer-cast` (63) + `misaligned-pointer` (2)
- **1 known root cause:** `ucl_hash.c:275` — incorrect function pointer in `ucl_parser_free` teardown  
  Minimal repro: `{"a":1}{"a":1}` — see [OSS_CVE_DISCLOSURE_libucl.md](OSS_CVE_DISCLOSURE_libucl.md)

**Sales honesty:** say *“2 sanitizer hygiene classes”* or *“1 known UBSan teardown issue (N variant inputs)”* — **not** “65 CVEs” or “65 unique bugs”.

Report metric to cite: `sanitizer_summary.by_subtype`, not raw `crashes` count.

---

## What the tests proved

1. **Standard 128/shard** works on pool (replay-verified).  
2. **libFuzzer is faster** (≈5–6× on cjson, ≈2× on libucl) — expected (in-process, coverage-guided).  
3. **Hunt surfaces UBSan hygiene** on parser-class targets in ways a short libFuzzer smoke may not persist as artifacts.  
4. **Same target can be CLEAN on both** (cjson) — Hunt depth ≠ guaranteed CVE advantage over libFuzzer in 90s.

## What the tests did *not* prove

- Hunt finds CVEs libFuzzer cannot (not shown on cjson).  
- Long-run libFuzzer with tuned corpus/options (we used 90s smoke parity).  
- Full L2 corpus-guided pool at 128 iter/shard (benchmark used local + short pool smoke).

---

## Customer / sales copy

**Say:**

- “Hunt Standard runs **128 verified execs per pool shard** with coordinator ASAN replay.”  
- “Default profile **asan+ubsan+lsan** — hygiene findings appear in the report appendix, separate from bounty lane.”  
- “libucl demo: known UBSan teardown class surfaced in minutes (informational, not CVE promise).”

**Do not say:**

- “Deeper than libFuzzer” or “more executions than libFuzzer.”  
- “65 vulnerabilities found” (libucl hygiene run).  
- “CVE guaranteed.”

---

## Operator commands

```bash
# C/C++ inventory compile gate
bash scripts/tests/hunt_inventory_cpp_gate.sh

# Rust inventory + serde_json ASAN pilot gate
bash scripts/tests/hunt_inventory_rust_gate.sh

# Full Hunt vs libFuzzer + pool Standard 128 benchmark
bash scripts/tests/hunt_standard128_live_benchmark.sh

# libucl crash dedup analysis (unique inputs vs root cause)
WALL_SEC=45 go run ./scripts/tests/tools/analyze_libucl_crashes.go

# Known libucl repro (stdin ASAN driver)
echo -n '{"a":1}{"a":1}' | .cache/oss-cve-bin/libucl-*.bin
```

---

## L2 bootstrap from libFuzzer (optional research lane)

**Honest pitch:** libFuzzer seeds give Hunt pool shards a **better starting corpus** (`hunt:{target_id}` namespace). They do **not** promise faster first CVE hit — soak #2 on libucl showed first-hit iter **1286 → 12586** with seeds (same root cause).

**Pipeline (after each libFuzzer session):**

```bash
# One catalog target (cjson, spl, parsello, jsmn, …)
TARGET=spl WALL_SEC=120 bash scripts/ops/hunt_import_libfuzzer_corpus.sh

# Obscure overnight batch
TARGETS="spl,parsello,centijson" bash scripts/ops/hunt_l2_bootstrap.sh

# Gate (unit + merge + optional 20s live import)
bash scripts/tests/hunt_l2_ab_gate.sh
```

Seeds land in `.cache/hunt-lf-seeds/{target}/` and merge automatically on Hunt campaign create / pool corpus bootstrap (`MergeLibFuzzerSeedCorpus`).

Targets **without** a dedicated libFuzzer harness use `stdin_subprocess_libfuzzer.c` (pipes into the same ASAN stdin driver Hunt replays).

---

## Roadmap idea (depth without pool rewrite)

### L2 bootstrap from libFuzzer research lane (implemented)

Optional bridge: after each libFuzzer session, import interesting inputs into Hunt L2 seed cache (`.cache/hunt-lf-seeds/{target_id}`). On Hunt campaign create / pool corpus bootstrap, seeds merge into `seed_byte_corpus` and `hunt:{target_id}` namespace persist.

**Honest caveat (soak #2, libucl):** seeds did **not** improve first-hit iteration (1286 → 12586). Do not promise “seeds = faster CVE.” Promise **better starting corpus for pool shards** — especially on obscure targets (spl, parsello).

```bash
# Single target (any catalog entry — dedicated or stdin subprocess harness)
TARGET=spl WALL_SEC=60 bash scripts/ops/hunt_import_libfuzzer_corpus.sh

# Batch obscure overnight targets
bash scripts/ops/hunt_l2_bootstrap.sh

# Gate: seeds ON vs OFF merge + pool bootstrap
bash scripts/tests/hunt_l2_seeds_ab_gate.sh
```

Pipeline: `cmd/hunt-lf-import` → `MergeLibFuzzerSeedCorpus` (campaign create, local run, `EnsureGuidedCorpusSeeded`). **Implemented** — see section above.

---

## Verdict

Document and show this comparison **yes** — it builds trust. Frame Hunt as **B2B depth tiers + verified fleet + hygiene reporting**, not as a libFuzzer replacement. The benchmark supports honest Standard 128 sales and a strong **libucl sanitizer demo**, while admitting libFuzzer wins raw throughput.
