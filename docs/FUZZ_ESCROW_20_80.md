# Fuzz campaign escrow (20/80 hybrid)

## Split

| Pool | Share | Pays when |
|------|-------|-----------|
| Runs | 20% | Each verified pool work submit (`PayFuzzRun`) |
| Bounty | 80% | First medium/high/critical finding (`PayFuzzBounty`, 5% fee to treasury) |

Unused bounty (no winner) is **refunded** to the campaign payer wallet on `FinalizeFuzzEscrow` when the campaign completes.

## API (local node)

- `POST /api/fuzz/campaigns` with `"budget_hmc": 10.0` opens escrow (requires `budget_runs` ≥ 8).
- `POST /api/fuzz/pool/settle` (admin): `{ "kind": "run|finding|finalize", "campaign_id", "miner_address", "severity" }`.
- `GET /api/fuzz/campaigns/{id}/escrow` — public escrow state.
- `GET /api/fuzz/campaigns/{id}/proof-bundle` — report + findings + escrow (report token).

## Coordinator

When `HACKME_COORDINATOR_ORDERS_URL` points at the node, pool submit relays settle automatically.

Workers pass `MINER_ADDRESS` / `miner_address` on submit.

## CLI

```bash
hackme-fuzzing campaign create -title "guard" -runs 100 -budget-hmc 5 -pool \
  -wasm-hex "$(xxd -p guard.wasm | tr -d '\n')"
```
