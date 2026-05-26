# Distributed pool fuzz (claim/submit)

Pool miners can run **fuzz work** on the coordinator alongside PoH mining.

## API (coordinator)

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| POST | `/api/fuzz/pool/campaigns` | admin | Register a pool-distributed campaign |
| POST | `/api/fuzz/work/claim` | worker | Lease one WASM check work item |
| POST | `/api/fuzz/work/submit` | worker | Submit check result |
| GET | `/api/fuzz/pool/stats` | public | Queue depth / runs done |

## Campaign config

```json
{
  "pool_distributed": true,
  "check_semantics": "detector",
  "wasm_check_hex": "...",
  "seed_corpus": [133452],
  "mutation_rounds": 0,
  "auto_runner": "0"
}
```

- **`pool_distributed`** — queue on coordinator; node autorunner skips local execute.
- **`check_semantics: detector`** — `check() != 0` is a finding (security guards).
- **`check_semantics: pow_gate`** — default; `check() == 0` is a finding (mining gates).

## Node → pool sync

On campaign create with `pool_distributed`, the node POSTs to the coordinator when set:

- `HACKME_POOL_COORDINATOR_URL` or `HACKME_COORDINATOR_URL`
- `HACKME_COORDINATOR_ADMIN_TOKEN` or `HACKME_POOL_COORDINATOR_ADMIN_TOKEN`

## Worker

```bash
COORD_URL=https://hackme.tech/pool/coordinator \
COORD_TOKEN=... \
WORKER_ID=rig-fuzz-01 \
go run ./cmd/workerfuzz
```

## Gate

```bash
bash scripts/ops/pool_fuzz_distributed_gate.sh
```
