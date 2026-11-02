# Orders publish → public pool (rc18.1)

## Problem

Local Orders (install → top up → wizard) is self-serve. Publishing a
**pool-distributed** campaign onto the public coordinator used to require the
full coordinator **admin** token, which must never ship in public installers.

## Solution

Scoped credential **`pool.orders.token`** /

`HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN` (hub) /

`HACKME_POOL_ORDERS_PUBLISH_TOKEN` (Orders node env).

| Capability | Worker token | Publish token | Admin token |
|------------|--------------|---------------|-------------|
| PoH claim/submit | ✅ | ❌ | ✅ |
| `POST /api/fuzz/pool/campaigns` | ❌ | ✅ (capped) | ✅ |
| Settle outbox GET + ACK (Orders pull) | ❌ | ✅ | ✅ |
| Hunt harness upload (`POST /api/fuzz/pool/hunt/harness`) + GET verify | ❌ | ✅ | ✅ |
| Settle replay / cleanup | ❌ | ❌ | ✅ |

### Publish caps (defaults)

| Cap | Default | Env |
|-----|---------|-----|
| `budget_runs` | ≤ **16384** (covers `hunt_heavy` 12000) | `HACKME_ORDERS_PUBLISH_MAX_BUDGET_RUNS` |
| `budget_seconds` | ≤ 86400 | `HACKME_ORDERS_PUBLISH_MAX_BUDGET_SECONDS` |
| Registers / IP / hour | 10 | `HACKME_ORDERS_PUBLISH_MAX_PER_HOUR` |
| Registers / IP / day | 40 | `HACKME_ORDERS_PUBLISH_MAX_PER_DAY` |

Requires Dig `dig_package` (`scan`/`starter`/`audit`/`pro`/`deep`/`enterprise`) or Hunt
`hunt_package` (`hunt_lite`/`hunt_standard`/`hunt_heavy`), or a Dig `guard_pack`.

## Security notes

- Publish token **must differ** from admin and worker (coordinator refuses to start otherwise).
- Release bundle **refuses** to ship publish == admin or publish == miner token.
- `pool.orders.token` loads into `HACKME_POOL_ORDERS_PUBLISH_TOKEN` only — never admin env.
- Shared installer secret: every Orders build with the same release token can register and
  settle-pull. Treat as a fleet API key; rotate via new gen + hub env + rebuild if leaked.
  Settle-pull only **applies** rows for local escrow; do not ACK foreign campaigns.
- Publish cannot: cancel/complete via status API **or** register upsert; settle replay;
  cleanup-stale/gates/repair-zombies; PoH attach (`attach_poh_order` stripped).
  Harness upload + GET verify are allowed (Windows attach flow uses publish then `harness_hash` on create).
- Publish cannot hijack existing non-`orders_publish` campaign IDs (HTTP 409).
- Settle outbox GET/ACK with publish **requires `campaign_id`** and only works for
  `orders_publish` campaigns (no global list / cross-tenant ACK of admin campaigns).
- Progress + findings mirror accept publish for `orders_publish` campaigns only.
- Publish IDs are ASCII `[A-Za-z0-9._-]` only (no `..`, no unicode lookalikes).
- Build env scrub drops `HACKME_*ORDERS_PUBLISH*` / coordinator worker+admin tokens.
- `pool.orders.token` must be mode ≤0600 (same as other secrets).

## Operator checklist

1. `bash scripts/ops/gen_orders_publish_token.sh`
2. Hub coordinator: set `HACKME_COORDINATOR_ORDERS_PUBLISH_TOKEN` (≠ admin, ≠ worker) and restart
3. Release: `HACKME_RELEASE_ORDERS_PUBLISH_TOKEN=…` when building Orders-capable bundles
4. Smoke: Orders node wizard with `pool_distributed` → campaign on pool stats → workers claim →
   settle-pull closes local escrow
5. **Do not deploy** until unit tests + this checklist pass

Local Dig/Hunt without public register still works with no publish token.
