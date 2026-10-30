# Fuzz campaigns — product & operator guide

HackMe **B2B security fuzz** runs on your **local node** (`127.0.0.1:8080`). The public site is catalog + onboarding only — orders, wallet, and reports stay on your machine.

**Customer entry:** `hackme-fuzzing wizard` · **Site guide:** [fuzz-guide.html](https://hackme.tech/fuzz-guide.html) · **HTTP tables:** [API.md](API.md) (Fuzz Campaigns)

---

## 1) Product SKUs (Scan · Dig)

CLI/API keys stay `scan` | `audit` | `deep`. Customer-facing names:

| SKU | CLI `--package` | Mode | HMC | Runs | Pool | Best for |
|-----|-----------------|------|-----|------|------|----------|
| **Scan** | `scan` | smoke | ~1 | 64 | local only | CI smoke, nightly guard |
| **Dig · Audit** | `audit` | smoke | ~5 | 256 | yes | Distributed smoke + attested report |
| **Dig · Deep** | `deep` | deep | ~40 (40/60 escrow) | 2048 | yes* | Paid depth, seed merge, miners paid more per run |

**Honest promise:** smoke = distributed smoke + attested report (20/80 escrow); deep = paid depth with 40/60 escrow + seed merge + higher replay sample. **Not** an OSS-Fuzz/CVE replacement. Reports expose `product_mode`, `dig_billing`, `replay_policy`, `seeds_merged`, hub exec cap.

**Dig depth v2:** richer pack `mutator_dict` profiles, tier power scheduling (Audit **mut_cap≥8** · Deep **≥14**), **Deep-only** external seeds in `.cache/dig-seeds/{pack}/`, cross-campaign corpus persist `pack:{id}`, CLEAN floor + canary anticheat, and customer report `dig_depth` / SKU honesty card.

**Hunt** (repo + ASAN on pool, 50/50 escrow) — **coverage-guided** on **0.1.0-rc18** channel:

| Engine | When |
|--------|------|
| **libFuzzer** | Fleet default (Lite / Standard / inventory) |
| **AFL++** | Deep / long profile when AFL present |
| **byte** | Explicit fallback only — **`HACKME_HUNT_ENGINE_BYTE_FALLBACK` default OFF** (soft-skip instead) |

**Pay (coordinator-proven):** full PayRun for **ASAN crash** and/or **`afl-showmap` bitmap edge delta**; structural/fingerprint buckets alone never unlock pay; CLEAN without proven map delta = **floor**. Worker-reported `edges_touched` is **untrusted for Hunt pay**. Spec: [HUNT_COVERAGE_ENGINES.md](HUNT_COVERAGE_ENGINES.md). Promise: distributed coverage-guided Hunt — **not** better than OSS-Fuzz.

Install engines (no miner AFL git clone): `bash scripts/ops/install_hunt_engines.sh --desktop`. Isolation: **bwrap** preferred; hub may run `HACKME_HUNT_ISOLATE_REQUIRE=0` if userns broken.

**Inventory languages (Phase 2.5):** **C, C++, and Rust (Phase A)** — scan `LLVMFuzzerTestOneInput` in `.c/.cpp` and `fuzz_target!` / `libfuzzer_sys` in `.rs`. C/C++ auto-compile sibling helpers with `clang`/`clang++` + ASAN. Rust **catalog** targets build with `cargo +nightly` AddressSanitizer stdin drivers (`serde_json` pipeline pilot; **`memchr`** / **`quick_xml`** unsafe-shaped). Customer Rust inventory **detect** works; auto-harness compile for arbitrary crates is catalog-only — see [HUNT_RUST_PHASE_A.md](HUNT_RUST_PHASE_A.md). **C#:** not in Hunt MVP.

| API | Purpose |
|-----|---------|
| `GET /api/hunt/packages` | Hunt Lite / Standard / Heavy presets |
| `GET /api/hunt/targets` | Curated OSS catalog (`upstream/oss_cve_targets.json`) |
| `POST /api/hunt/inventory` | Admin: scan local path for `LLVMFuzzerTestOneInput` / Rust `fuzz_target!` + **pack-map suggest** |
| `POST /api/hunt/pack-suggest` | Admin: Dig/Hunt pack hints for one path |
| `POST /api/hunt/repo/pin` | Admin: pin local path or shallow git clone |
| `POST /api/hunt/template/preview` | Admin: check if template Accept is required |
| `POST /api/hunt/harness/build` | Admin: ASAN build inventory harness → `.cache/hunt-harness/{hash}.bin` |
| `POST /api/hunt/harness/publish` | Admin: publish harness blob to node + coordinator pool |
| `GET /api/fuzz/pool/hunt/harness/{hash}` | Workers: fetch published ASAN harness (coordinator) |
| `POST /api/hunt/campaigns` | Create Hunt campaign + 50/50 escrow (catalog or inventory) |
| `POST /api/hunt/campaigns/{id}/run-local` | Admin: node-local ASAN smoke |

CLI: `hackme-fuzzing hunt pin|inventory|template|build|create|pack-suggest|packages|targets`

**Ideal customer-repo path (one script):** inventory → ASAN build → Hunt Standard pool → token report:

```bash
ADMIN_TOKEN=… NODE=http://127.0.0.1:8080 \
  bash scripts/ops/place_customer_hunt.sh /path/to/repo fuzzing/target.c 16
# E2E gate (local coordinator+node): bash scripts/tests/hunt_customer_repo_e2e.sh
```

Spec: [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md) · engines: [HUNT_COVERAGE_ENGINES.md](HUNT_COVERAGE_ENGINES.md) · live bench + sales framing: [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md) (**2026-10-11** coverage re-bench — old byte ~100-vs-700 charts are archive only). Pool leases an **engine wall** (`engine_lease_sec`; fleet ~60s · deep ~180s) with `exec_per_unit=1` for coverage engines; workers upload session-new corpus via `POST /api/fuzz/work/corpus_delta`; coordinator ASAN-oneshots + **afl-showmap** prove. Linux miners **auto-install** clang/AFL++/bwrap on install/update in `main` (published **rc18** installers auto-run `install_hunt_engines.sh`). Opt out: `HACKME_SKIP_HUNT_ENGINES=1`. **Byte path (opt-in only):** L1 mutating shards (`iterations_per_shard` Lite **48** · Standard **192** · Heavy **256**, same as `internal/hunt/depth.go`) remain for explicit `hunt_engine=byte` / fallback-on labs — not the fleet default. **L2 corpus:** campaign corpus grows across shards (`hunt:{target_id}` namespace persist). **Optional research seed import:** `.cache/hunt-lf-seeds/{target}` — better starting corpus, not guaranteed faster first hit. **Dig** seed-from-research stays **OFF** by default (`HACKME_POOL_SEED_FROM_RESEARCH`). **Overnight local:** `hunt_local_runner` autorunner (no pool). **Inventory pool** uses harness publish — workers download ASAN binary from coordinator. Missing harness → soft-skip / auto-cancel (~15m), not fake CLEAN pay.

**Hunt packages (budgets):** Lite **20 HMC** · Standard **60** · Heavy **150+**. Coverage fleet depth = engine wall + proven edges/crashes, not brochure exec/shard counts. **C pilot catalog:** `spl` — `bash scripts/ops/hunt_pilot_external_1h.sh`. **Rust catalog (Phase A):** `serde_json`, `memchr`, `quick_xml` — [HUNT_RUST_PHASE_A.md](HUNT_RUST_PHASE_A.md). Catalog size: **56** targets in `upstream/oss_cve_targets.json` (mostly C/C++ stdin ASAN + 3 Rust).

\* **Distributed pool cap:** code default `HACKME_POOL_EXEC_PER_UNIT_CAP=64`; **production hub uses 256**. Local Deep still **512**. Hunt pool shards use package `iterations_per_shard` up to **256**. Dig submit uses **sampled + crash-first replay** — clean multi-exec hygiene may skip full segment replay; crash/found/sanitizer claims always full-replay (cannot forge bounty).

**Hunt sanitizer profile (default):** `asan+ubsan+lsan` — LSan via `ASAN_OPTIONS=detect_leaks=1` (disable with `hunt_detect_leaks: false` or env `HACKME_HUNT_DETECT_LEAKS=0`). UBSan/LSan findings use `sanitizer_informational` with explicit subtypes (`shift-overflow`, `null-deref`, `direct-leak`, …) in report hygiene section — not bounty-eligible.

```bash
export HACKME_ADMIN_TOKEN=…   # local node
hackme-fuzzing wizard --pack filter_utf8 --package audit --title "FluxTap preflight"
hackme-fuzzing wizard --wasm ./guard.wasm --package deep --public-proof
hackme-fuzzing packs   # list ready packs
```

Wizard prints: `campaign_id`, `customer_report_token`, `report_url`, `gate_url`, optional `proof_url`.

---

## 2) Ready packs (detectors)

| Pack | What it catches |
|------|-----------------|
| **secrets** | AWS/API key patterns in byte inputs |
| **script_bounds** | Bitcoin-class script push bound violations |
| **filter_utf8** | Invalid UTF-8 + operator index skew (FluxTap-class display filter panic on `\xc7=`) |
| **parser_expat** | XML byte corpus; native ASAN on pinned libexpat (`native_repro_mode: oss_upstream`) |
| **bounds_smoke** | Scan-tier numeric range/stride guard smoke |
| **overflow_smoke** | Scan-tier wrapping-multiply overflow smoke |
| **state_smoke** | Scan-tier FSM transition guard smoke |

Pack-aware budgets override generic package defaults (see `hackme-fuzzing packs --json`).

---

## 3) Coverage semantics (honest)

| `coverage_kind` | Where | Meaning |
|-----------------|-------|---------|
| **wasm_edge_bitmap** | `secrets`, `filter_utf8`, `script_bounds` | Instrumented guard writes a **256-byte edge counter bitmap** at linear memory offset **8192** after each `check_bytes` — used for **guided scheduling**, not AFL path coverage |
| **input_fingerprint** | `parser_expat`, legacy guards | Hash/fingerprint buckets only |

OSS research (libFuzzer on nghttp2/expat) is a **separate lane** — do not conflate with customer `wasm_edge_bitmap`.

---

## 4) Guard ABI

- Legacy: `check(i64) -> i32`
- Bytes mode: `check_bytes(ptr, len) -> i32` with `input_mode=bytes`
- Template: `tasks/sources/security/rust_customer_bytes_guard_template.rs`

---

## 5) Deliverables

1. **HTML report** — `GET /api/fuzz/campaigns/{id}/report` + `X-Hackme-Report-Token`
2. **CI gate** — `GET /api/fuzz/campaigns/{id}/gate?max_critical=0&max_high=0`
3. **Pulse** — live progress for dashboards
4. **Proof of Fuzz** (opt-in) — `wizard --public-proof` → `/proof/{id}` + badge (crash-gate only; secrets redacted)

See [CUSTOMER_FUZZ_DELIVERABLES.md](CUSTOMER_FUZZ_DELIVERABLES.md).

---

## 6) Pool distributed fuzz

When `pool_distributed: true`, hub `workerfuzz` / hybrid `workerpoh` claims work via `/api/fuzz/work/claim`.

**Anticheat (rc17 / Dig + Hunt pool):**

- Guided campaigns freeze `corpus_seeds` + anchor input at **claim**
- Tier defaults: `power_mut_cap` scan **2** · audit **6** · deep **12** (pool segment mutation depth)
- Wizard sends `mutation_rounds`, `coverage_guided`, `guided_scheduling`, `power_mut_cap`, `corpus_persist` for Dig tiers
- Pack `mutator_dict` splices domain tokens (secrets, XML, UTF-8 skew)
- **Cross-campaign corpus persist** (`fuzz_corpus_namespace`): audit/deep guided campaigns import prior seeds for the same `guard_pack` namespace on new campaigns
- Submit requires matching `segment_exec_done`
- **Crash/found/sample:** coordinator full-segment replay; **clean Dig hygiene:** hybrid auth accept without full replay (no findings minted)
- Invalid WASM → reject; incomplete segment → reject
- Worker lease scales with segment wall time (not fixed 30s)
- Submit nonce reserved at signature validate (anti-replay)
- Optional: `claim_batch` / `submit_batch` (≤16), claim-light corpus sha, worker prefetch
- Coverage-guided Hunt engines (**libFuzzer** fleet default, **AFL++** Deep when AFL present): lease = engine wall; pay for coordinator-proven **showmap edges** + **ASAN crash** only ([HUNT_COVERAGE_ENGINES.md](HUNT_COVERAGE_ENGINES.md)); **`HACKME_HUNT_ENGINE_BYTE_FALLBACK` default OFF** → soft-skip (not silent byte dilution)
- Hybrid Hunt research slot (escape `HACKME_WORKER_RESEARCH_SLOT=0`): optional short LF persist → corpus-delta + ASAN-replayed crashes ([POOL_WORKER_RESEARCH_SLOT.md](internal/POOL_WORKER_RESEARCH_SLOT.md)); does **not** enable Dig `HACKME_POOL_SEED_FROM_RESEARCH` (stays OFF)

**Safe fleet tiers:** Scan (1) · Audit (64) · Deep hub **cap 256** (local 512). Fleet ~25–35 dig/hybrid workers typical.

Details: [POOL_FUZZ_DISTRIBUTED.md](POOL_FUZZ_DISTRIBUTED.md).

---

## 7) Hybrid mining (unchanged)

GPU rigs run PoH by default; fuzz fills idle/backpressure slots (`HACKME_WORKER_HYBRID_FUZZ=1`). PoH GH/s is primary; fuzz adds coordinator accrual on completed pool work — not a separate “mining algorithm.”

---

## 8) Release gate (operators)

```bash
ADMIN_TOKEN=… BASE=http://127.0.0.1:8080 bash scripts/ops/fuzz_release_gate.sh
bash scripts/ops/fuzz_b2b_final_gate.sh   # B2B wizard + packs smoke
MODE=lang_static bash scripts/tests/run_daily.sh
```

---

## 9) Autorunner & env

| Env | Role |
|-----|------|
| `HACKME_FUZZ_AUTORUN` | Server-side campaign ticking (default on) |
| `HACKME_FUZZ_AUTORUN_TICK_SEC` | Tick interval 1–60s |
| `HACKME_FUZZ_ARTIFACT_DIR` | Artifact root |
| Per-campaign `config.auto_runner=false` | Skip autorunner for one row |

---

## 10) Related docs

- [DEVELOPERS_FUZZING.md](DEVELOPERS_FUZZING.md) — localhost model, auth, CLI
- [FUZZ_ESCROW_20_80.md](FUZZ_ESCROW_20_80.md) — Dig/Scan 20/80 split
- [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md) — Hunt 50/50 (Phase 1–2)
- [HUNT_COVERAGE_ENGINES.md](HUNT_COVERAGE_ENGINES.md) — libFuzzer/AFL++ fleet, pay prove, byte fallback OFF
- [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md) — honest limits (not better than OSS-Fuzz)
- [FUZZING_B2B_SECURITY_VERDICT.md](archive/verdicts/FUZZING_B2B_SECURITY_VERDICT.md) — threat model
