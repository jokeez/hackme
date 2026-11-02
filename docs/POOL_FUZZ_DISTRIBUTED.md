# Distributed pool fuzz (claim/submit)

Pool miners run **fuzz work** on the coordinator alongside PoH mining (hybrid default ON).

---

## Storage (hub)

| Env | File |
|-----|------|
| `HACKME_COORDINATOR_DB` | PoH / work / peers (default `data/coordinator.db`) |
| `HACKME_COORDINATOR_FUZZ_DB` | Fuzz campaigns / queue / findings (hub: `/opt/hackme/data/coordinator_fuzz.db`) |

If `HACKME_COORDINATOR_FUZZ_DB` is unset, fuzz shares the main DB (compat).

---

## API (coordinator)

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/api/fuzz/pool/campaigns` | admin | Register pool-distributed campaign |
| POST | `/api/fuzz/work/claim` | worker | Lease one work item (+ frozen corpus on guided campaigns) |
| POST | `/api/fuzz/work/claim_batch` | worker | Lease up to **16** items (`limit`); same auth/lease rules per item |
| POST | `/api/fuzz/work/submit` | worker | Submit segment result (+ hybrid Ed25519 when escrow payout) |
| POST | `/api/fuzz/work/submit_batch` | worker | Submit up to **16** items; each row auth’d; foreign leases fail that row only |
| GET | `/api/fuzz/pool/stats` | public | Queue depth / runs done |

Worker batch: `HACKME_WORKER_BATCH_CLAIM=N` (default **2**, max 16). Dig WASM stays warm across shards via sandbox compile cache (`HACKME_WORKER_WARM_HARNESS=0` to disable pre-validate). Prefetch next Dig claim while current runs: `HACKME_WORKER_PREFETCH` default ON (`=0` to disable). Claim-light: `HACKME_POOL_CLAIM_LIGHT` default ON when `HACKME_POOL_CORPUS_DIR` is set. Idle claim negative cache: `HACKME_POOL_EMPTY_CLAIM_MS` (default **400**).

Claim JSON includes: `exec_per_unit`, `max_input_bytes`, `coverage_kind`, `corpus_snapshot_sha256` (when guided). Fat `corpus_seeds[]` may be omitted when claim-light is on (`corpus_light: true` + sha) — worker fetches via `POST /api/fuzz/work/corpus_snapshot` under its lease. Hunt claims already prefer `harness_hash` / `harness_content_sha256`.

---

## Campaign config

```json
{
  "pool_distributed": true,
  "check_semantics": "detector",
  "wasm_check_hex": "...",
  "guided_scheduling": true,
  "input_mode": "bytes",
  "exec_per_unit": 64,
  "coverage_kind": "wasm_edge_bitmap",
  "seed_byte_corpus": ["c73d"],
  "auto_runner": "0"
}
```

- **`pool_distributed`** — queue on coordinator; node autorunner skips local execute.
- **`check_semantics: detector`** — `check() != 0` / detector hit is a finding.
- **`check_semantics: pow_gate`** — default PoH gate semantics.

---

## exec_per_unit & tiers

| Tier | Local autorunner | Pool worker (distributed) |
|------|------------------|---------------------------|
| Scan | 1 | 1 |
| Audit | 64 | min(64, hub cap) |
| Deep | 512 | **min(512, hub cap)** — code default cap **64**; production hub currently **`HACKME_POOL_EXEC_PER_UNIT_CAP=256`** |

Cap lives in `internal/poolfuzz/pool_exec.go`. **Do not** advertise pool Deep **512** as miner-proved on hub — effective depth is the env cap (256 on hackme.tech today); crash/found paths still **full-replay**.

---

## Anticheat (submit path) — sampled + crash-first + CLEAN floor/canary

1. **Claim** freezes guided anchor input + `corpus_seeds[]` snapshot.
2. Worker returns `segment_exec_done` matching `exec_per_unit` (and should send `edges_touched` for bitmap campaigns).
3. **Full coordinator segment replay** when the worker claims crash/found/sanitizer fail, or when a clean submit is randomly sampled.
4. **Clean Dig hygiene** (multi-exec): accept via hybrid Ed25519 submit auth **without** full segment replay — **never** mints findings/bounty on this path — but only after **CLEAN floor** + optional **canary** checks.
5. Worker lease scales: `exec × check_timeout + slack` (max 600s).
6. Hybrid submit: Ed25519 PoP + nonce reserved at validate (replay blocked).

### CLEAN floor (Dig hygiene_skip)

Rejects empty/fake CLEAN without paying. Defaults stay conservative so honest miners keep earning.

| Env | Default | Meaning |
|-----|---------|---------|
| `HACKME_POOL_CLEAN_FLOOR` | ON | Master switch |
| `HACKME_POOL_CLEAN_MIN_DURATION_MS` | 1 | Absolute duration floor for multi-exec CLEAN |
| `HACKME_POOL_CLEAN_MIN_MS_PER_EXEC` | 0→1 | Extra per-exec ms; auto-defaults to 1 when edges require is active |
| `HACKME_POOL_CLEAN_MIN_EDGES` | 1 | Min `edges_touched` when worker reports it (smoke) |
| `HACKME_POOL_CLEAN_MIN_EDGES_DEEP` | 2 | Min edges for Dig Deep SKU |
| `HACKME_POOL_CLEAN_EDGES_REQUIRE` | auto | `auto` = ON at ≥90% fleet report rate; `1` force; `0` off |

### Canary / challenge shards (Dig guided + Hunt)

With small probability Dig locks a pack-known detector-hit input; Hunt optionally locks `canary_input_hex` / `hunt_canary_input_hex`. Honest workers report the finding/crash. CLEAN without hitting it → `canary_miss` reject.

| Env | Default | Meaning |
|-----|---------|---------|
| `HACKME_POOL_CANARY` | ON | Master switch (Dig + Hunt) |
| `HACKME_POOL_CANARY_PCT` | 2 | % of guided Dig claims marked canary (start low) |
| `HACKME_HUNT_CANARY_PCT` | (falls back to POOL) | % of Hunt claims; requires known-crash hex in campaign config |

Without Hunt hex in config, Hunt canaries stay off.

### Replay sample env

| Env | Default | Meaning |
|-----|---------|---------|
| `HACKME_POOL_REPLAY_SAMPLE_PCT` | 5 | Audit clean-submit sample % |
| `HACKME_POOL_REPLAY_SAMPLE_PCT_SCAN` | 1 | Scan package sample % |
| `HACKME_POOL_REPLAY_SAMPLE_PCT_DEEP` | 40 | Deep package sample % |
| `HACKME_POOL_HYGIENE_PROVE` | ON | Dig CLEAN: coordinator WASM prove before PayRun |
| `HACKME_POOL_HYGIENE_SEGMENT_PROVE` | auto | `auto` = full segment prove on Dig hygiene for deep or escrow multi-exec; `1` force Dig multi-exec; `0` single-anchor only |
| `HACKME_POOL_REPLAY_SAMPLE_PCT_HUNT` | 100 | Hunt clean path (full verify by default) |
| `HACKME_POOL_REPLAY_CRASH_ALWAYS` | 1 | Document crash-first (claims always replay) |
| `HACKME_POOL_FULL_REPLAY` | off | Ops rollback: always full Dig replay |
| `HACKME_POOL_REPLAY_SAMPLE_SEED` | empty | Deterministic sample bit (tests) |

Submit JSON may include `replay_status`: `hygiene_skip` · `sample` · `crash_claim` · `finding_claim` · … and optional `edges_touched`.

### SKU honesty (reports)

Customer reports expose `product_mode` (`smoke`|`deep`), `replay_policy`, `replay_sample_pct`, `lf_budget_sec`, `seeds_merged`, hub exec cap truth, and a promise note (**not** OSS-Fuzz/CVE replacement). **`HACKME_POOL_SEED_FROM_RESEARCH` default OFF** (Dig seed feed — do not enable casually). Dig Deep may merge **external** pack seeds; that is separate from the research-seed feed.

---

## Hunt coverage engines (pool)

Fleet Hunt leases are **coverage-guided** (`hunt_engine=libfuzzer` default · `aflpp` Deep when AFL present). Claim carries `engine_lease_sec` / `engine_profile`; submit pay uses coordinator **ASAN oneshot + afl-showmap** — structural ≠ pay; CLEAN without delta = floor; byte fallback default **OFF**. Isolation: **bwrap** preferred; hub may set `HACKME_HUNT_ISOLATE_REQUIRE=0` if userns broken. Install: `scripts/ops/install_hunt_engines.sh` (no miner AFL git clone). Full contract: [HUNT_COVERAGE_ENGINES.md](HUNT_COVERAGE_ENGINES.md).

---

## Coverage on pool (Dig WASM)

Instrumented Dig guards write **`wasm_edge_bitmap`** at linear memory offset **8192** (256 bytes). Used for scheduling buckets — **not** AFL coverage, and AFL/libFuzzer never enter Dig. Pro-tier `max_input_bytes=4096` inputs stay below the bitmap region (input @ offset 8).

---

## Node → pool sync

On campaign create with `pool_distributed`, the node POSTs to the coordinator when set:

- `HACKME_POOL_COORDINATOR_URL` or `HACKME_COORDINATOR_URL`
- `HACKME_COORDINATOR_ADMIN_TOKEN` or `HACKME_POOL_COORDINATOR_ADMIN_TOKEN`

---

## Worker

### Hybrid (fleet default — GPU desktops / release miners)

One `worker_id` with live GH/s that also digs fuzz (**default ON**):

```bash
COORD_URL=https://hackme.tech/pool/coordinator \
COORD_TOKEN=... \
WORKER_ID=worker-my-pc \
HACKME_MINER_ED25519_SEED_HEX=... \
./bin/workerpoh-cuda
```

Escape hatch: `HACKME_WORKER_HYBRID_FUZZ=0`.

Optional: `HACKME_WORKER_HYBRID_FUZZ_MODE=process`, `HACKME_WORKER_HYBRID_FUZZ_CONCURRENCY=1` (hard cap 2), `HACKME_WORKER_HYBRID_FUZZ_BACKPRESSURE_PCT=35`.

Do **not** also run `workerfuzz_autostart.sh` with a `*-fuzz` id on the same host when hybrid is on.

### Standalone fuzz digger

```bash
COORD_URL=https://hackme.tech/pool/coordinator \
COORD_TOKEN=... \
WORKER_ID=rig-fuzz-01 \
go run ./cmd/workerfuzz
```

---

## Throughput: N miners vs one local node

- **N miners** ≈ **N×** parallel work items (customer campaigns prioritized over bootstrap).
- **Local autorunner** runs full Deep (512 exec/unit) on one machine — higher per-item depth, lower fleet parallelism.
- PoH GH/s dominates rig economics; fuzz adds escrow accrual on completed pool units, not a replacement for mining.

---

## Gate

```bash
bash scripts/ops/pool_fuzz_distributed_gate.sh
bash scripts/ops/run_customer_pool_smoke.sh   # Scan/Audit smoke
```

Worker-local research slot (Stage D; escape `HACKME_WORKER_RESEARCH_SLOT=0`): optional short LF persist → `POST /api/fuzz/work/corpus_delta` (lease-bound; crashes ASAN-replayed). Distinct from the primary **coverage-engine lease** (LF/AFL++ wall). Does not enable Dig `HACKME_POOL_SEED_FROM_RESEARCH`. See [POOL_WORKER_RESEARCH_SLOT.md](internal/POOL_WORKER_RESEARCH_SLOT.md) · [HUNT_COVERAGE_ENGINES.md](HUNT_COVERAGE_ENGINES.md).

See also [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md).
