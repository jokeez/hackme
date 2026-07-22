# Fuzz campaign escrow (20/80 hybrid)

## Split

| Pool | Share | Pays when |
|------|-------|-----------|
| Runs | 20% | Each verified pool work submit (`PayFuzzRun`) |
| Bounty | 80% | First medium/high/critical finding (`PayFuzzBounty`, 5% fee to treasury) |
| Unique crash bonus | ≤1% of bounty (cap 0.01 HMC) | First **crash-class** finding (`PayFuzzCrashBonus` / settle kind `crash_bonus`) — does **not** close bounty; detector/property noise does not qualify |

Unused bounty (no winner) is **refunded** to the campaign payer wallet on `FinalizeFuzzEscrow` when the campaign completes. Crash bonus already paid is not refunded.

## Live fields (`GET .../escrow`, also on campaign GET + pulse)

| Field | Meaning |
|-------|---------|
| `spent_runs_hmc` | Runs pool paid so far (= `runs_paid_hmc`) |
| `locked_bounty_hmc` | Bounty still locked (pool − main bounty − crash bonus) |
| `runs_remaining_hmc` | Unspent runs pool |
| `refundable_hmc` | What would refund on finalize/cancel now |
| `refund_path` | Machine-readable path: `finalize_or_cancel_refunds_unused_runs_and_locked_bounty` / `…_runs_only` / `already_closed` |

## API (local node)

- `POST /api/fuzz/campaigns` with `"budget_hmc": 10.0` opens escrow (requires `budget_runs` ≥ 8, **budget_hmc ≥ 0.5 HMC**, per-run slice ≥ 0.0001 HMC).
- `POST /api/fuzz/pool/settle` (admin): `{ "kind": "run|finding|crash_bonus|finalize", "campaign_id", "miner_address", "severity", "event_id" }`.
- `GET /api/fuzz/campaigns/{id}/escrow` — escrow state + live fields (auth: report token or admin; not public).
- `GET /api/fuzz/campaigns/{id}/proof-bundle` — report + findings + escrow (report token).

## Coordinator settle `event_id`

Stable key for idempotent run/finding/crash/finalize credits:

`outbox:<campaign_id>:<outbox_row_id>`  
example: `outbox:cve-libheif-day2:1842`

Do **not** use bare `outbox:<id>` — low ids collide across campaigns / coordinator resets when customer nodes already applied bootstrap `outbox:1..N`.

Ops (no DB wipe): `scripts/ops/bump_fuzz_settle_outbox_seq.sh` (hub sequence floor) + `scripts/ops/replay_fuzz_escrow_settle.sh <campaign_id>` for unpaid escrow after deploy.

## Money path clarity

| Signal | What it is | Not |
|--------|------------|-----|
| Coordinator `workers[*].payout_hmc` / `total_payout_hmc` | Off-chain **PoH/work** accrual | Fuzz escrow credits |
| Fuzz campaign escrow (`runs_paid` / bounty) | On-node **fuzz** settle via `ApplyFuzzSettleOnce` | PoH pool settlement |

A fuzz worker can show `payout_hmc=0` while still earning from campaign escrow.

## CLI

```bash
hackme-fuzzing campaign create -title "guard" -runs 100 -budget-hmc 5 -pool \
  -wasm-hex "$(xxd -p guard.wasm | tr -d '\n')"
```
