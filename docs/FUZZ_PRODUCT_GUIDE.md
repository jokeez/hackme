# Fuzz campaigns — product & operator guide

HackMe **B2B security fuzz** runs on your **local node** (`127.0.0.1:8080`). The public site is catalog + onboarding only — orders, wallet, and reports stay on your machine.

**Customer entry:** `hackme-fuzzing wizard` · **Site guide:** [fuzz-guide.html](https://hackme.tech/fuzz-guide.html) · **HTTP tables:** [API.md](API.md) (Fuzz Campaigns)

---

## 1) Product SKUs (Scan · Dig)

CLI/API keys stay `scan` | `audit` | `deep`. Customer-facing names:

| SKU | CLI `--package` | HMC | Runs | Pool | Best for |
|-----|-----------------|-----|------|------|----------|
| **Scan** | `scan` | ~1 | 64 | local only | CI smoke, nightly guard |
| **Dig · Audit** | `audit` | ~5 | 256 | yes | Protocol guards, DeFi invariants |
| **Dig · Deep** | `deep` | ~25 | 2048 | yes* | Byte corpus, hours-scale campaign |

**Dig depth v2 (2026-09):** richer pack `mutator_dict` profiles, tier power scheduling (Audit **mut_cap≥8** · Deep **≥14**), optional external seeds in `.cache/dig-seeds/{pack}/`, cross-campaign corpus persist `pack:{id}`, and customer report `dig_depth` card + expanded `human_summary`.

**Hunt** (repo + ASAN on pool, 50/50 escrow) — Phase 2 on `feature/hunt-mvp`:

**Inventory languages (Phase 2.5):** **C, C++, and Rust (Phase A)** — scan `LLVMFuzzerTestOneInput` in `.c/.cpp` and `fuzz_target!` / `libfuzzer_sys` in `.rs`. C/C++ auto-compile sibling helpers with `clang`/`clang++` + ASAN. Rust catalog targets build with `cargo +nightly` AddressSanitizer stdin drivers (pilot: `serde_json`). Customer Rust inventory **detect** works; auto-harness compile for arbitrary crates is catalog-only — see [HUNT_RUST_PHASE_A.md](HUNT_RUST_PHASE_A.md). **C#:** not in Hunt MVP.

| API | Purpose |
|-----|---------|
| `GET /api/hunt/packages` | Hunt Lite / Standard presets |
| `GET /api/hunt/targets` | Curated OSS catalog (`upstream/oss_cve_targets.json`) |
| `POST /api/hunt/inventory` | Admin: scan local path for `LLVMFuzzerTestOneInput` + **pack-map suggest** |
| `POST /api/hunt/pack-suggest` | Admin: Dig/Hunt pack hints for one path |
| `POST /api/hunt/repo/pin` | Admin: pin local path or shallow git clone |
| `POST /api/hunt/template/preview` | Admin: check if template Accept is required |
| `POST /api/hunt/harness/build` | Admin: ASAN build inventory harness → `.cache/hunt-harness/{hash}.bin` |
| `POST /api/hunt/harness/publish` | Admin: publish harness blob to node + coordinator pool |
| `GET /api/fuzz/pool/hunt/harness/{hash}` | Workers: fetch published ASAN harness (coordinator) |
| `POST /api/hunt/campaigns` | Create Hunt campaign + 50/50 escrow (catalog or inventory) |
| `POST /api/hunt/campaigns/{id}/run-local` | Admin: node-local ASAN smoke |

CLI: `hackme-fuzzing hunt pin|inventory|template|build|create|pack-suggest|packages|targets`

Spec: [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md) · **vs libFuzzer:** [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md) (live benchmark, honest depth). Pool CPU shards + coordinator ASAN replay — Phase 1c. **L1 mutating shards:** each shard runs `iterations_per_shard` (Lite **32** · Standard **128** · Heavy **256**) deterministic byte mutations from the claim anchor; coordinator replays the full chain on submit (fake-crash reject unchanged). **L2 corpus-guided:** claim freezes `corpus_seeds` + guided anchor; campaign corpus grows across shards (`hunt:{target_id}` namespace persist). **Optional L2 bootstrap:** import libFuzzer research corpus into `.cache/hunt-lf-seeds/{target}` (`scripts/ops/hunt_import_libfuzzer_corpus.sh`) — better starting pool corpus, not guaranteed faster first hit. **Overnight local:** `hunt_local_runner` autorunner (no pool) — ticks until `hunt_local_budget_iterations` / wall limit. **Domain dict:** `mutator_dict` per target class (JSON/XML/INI/TOML/msgpack). **Inventory pool** uses harness publish (`harness_fetch_path`) — workers download ASAN binary from coordinator.

**Hunt pool depth (per shard):** Lite **32** · Standard **128** · Heavy **256** exec/shard (`iterations_per_shard`). **Pilot catalog:** `spl` (iacobucci/spl, Feb 2026 JSON combinator) — `bash scripts/ops/hunt_pilot_external_1h.sh`. **Overnight local** (non-pool campaigns): autorunner ticks `hunt_local_tick_iterations` (default 2000) until package budget — Lite **20k/1h** · Standard **200k/8h** · Heavy **500k/12h**. **Domain mutator dict** auto-applied per catalog target (JSON/XML/INI/TOML/msgpack splice tokens).

\* **Distributed pool cap:** on hub workers, `exec_per_unit` is capped at **64** per work item for generic fuzz; Hunt pool shards use package `iterations_per_shard` up to **256** on coordinator replay path. Coordinator **replays** the segment on submit — miners do not cryptographically attest every exec.

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

**Anticheat (rc16 / Phase 2):**

- Guided campaigns freeze `corpus_seeds` + anchor input at **claim**
- Tier defaults: `power_mut_cap` scan **2** · audit **6** · deep **12** (pool segment mutation depth)
- Wizard sends `mutation_rounds`, `coverage_guided`, `guided_scheduling`, `power_mut_cap`, `corpus_persist` for Dig tiers
- Pack `mutator_dict` splices domain tokens (secrets, XML, UTF-8 skew)
- **Cross-campaign corpus persist** (`fuzz_corpus_namespace`): audit/deep guided campaigns import prior seeds for the same `guard_pack` namespace on new campaigns
- Submit requires matching `segment_exec_done` and coordinator full-segment replay
- Invalid WASM → reject; incomplete segment → reject
- Worker lease scales with segment wall time (not fixed 30s)
- Submit nonce reserved at signature validate (anti-replay)

**Safe fleet tiers:** Scan (1 exec) and Audit (64 exec). Treat pool Deep as **cap-64** until attestation ships.

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
- [HUNT_VS_LIBFUZZER.md](HUNT_VS_LIBFUZZER.md) — live depth benchmark vs libFuzzer (honest limits)
- [FUZZING_B2B_SECURITY_VERDICT.md](FUZZING_B2B_SECURITY_VERDICT.md) — threat model
