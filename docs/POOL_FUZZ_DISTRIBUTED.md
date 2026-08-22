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
| POST | `/api/fuzz/work/submit` | worker | Submit segment result (+ hybrid Ed25519 when escrow payout) |
| GET | `/api/fuzz/pool/stats` | public | Queue depth / runs done |

Claim JSON includes: `exec_per_unit`, `max_input_bytes`, `coverage_kind`, `corpus_seeds`, `corpus_snapshot_sha256` (when guided).

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
| Audit | 64 | 64 |
| Deep | 512 | **64 (cap)** |

Cap lives in `internal/poolfuzz/pool_exec.go` until sampled worker attestation exists. **Do not** advertise pool Deep 512 as fully miner-proved — coordinator **replays** the segment on submit.

---

## Anticheat (submit path)

1. **Claim** freezes guided anchor input + `corpus_seeds[]` snapshot.
2. Worker returns `segment_exec_done` matching `exec_per_unit`.
3. Coordinator replays all execs; rejects invalid WASM, incomplete segments, input/corpus drift.
4. Worker lease scales: `exec × check_timeout + slack` (max 600s).
5. Hybrid submit: Ed25519 PoP + nonce reserved at validate (replay blocked).

This is **coordinator replay anticheat**, not per-exec miner attestation.

---

## Coverage on pool

Instrumented guards write **`wasm_edge_bitmap`** at linear memory offset **8192** (256 bytes). Used for scheduling buckets — **not** AFL coverage. Pro-tier `max_input_bytes=4096` inputs stay below the bitmap region (input @ offset 8).

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

See also [FUZZ_PRODUCT_GUIDE.md](FUZZ_PRODUCT_GUIDE.md).
