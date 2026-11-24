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
| Registers / IP / hour | 600 | `HACKME_ORDERS_PUBLISH_MAX_PER_HOUR` |
| Registers / IP / day | 3000 | `HACKME_ORDERS_PUBLISH_MAX_PER_DAY` |
| `budget_hmc` | ≤ **500** (SKU floor still applies) | `HACKME_ORDERS_PUBLISH_MAX_BUDGET_HMC` |

Requires Dig `dig_package` (`scan`/`starter`/`audit`/`pro`/`deep`/`enterprise`) or Hunt
`hunt_package` (`hunt_lite`/`hunt_standard`/`hunt_heavy`), or a Dig `guard_pack`.

## Security notes

- Publish token **must differ** from admin and worker (coordinator refuses to start otherwise).
- Release bundle **refuses** to ship publish == admin or publish == miner token.
- `pool.orders.token` loads into `HACKME_POOL_ORDERS_PUBLISH_TOKEN` only — never admin env.
- Shared installer secret: every Orders build with the same release token can register and
  settle-pull. Treat as a fleet API key; rotate via new gen + hub env + rebuild if leaked.
  Tenant binding: non-empty `owner_ref` + `X-Hackme-Orders-Owner-Ref` (must match) and
  per-publish `orders_owner_secret` / `X-Hackme-Orders-Owner-Secret`. Prepaid
  `budget_hmc` + `prepaid_attestation` required before work enqueue. **D1 / minting hub**
  (`HACKME_CHAIN_MINTING_HUB=1` or `HACKME_ORDERS_REQUIRE_HUB_PREPAID_TICKET=1` on the
  coordinator): forgeable publish-token self-MAC is refused — require hub-signed
  `hubv1:` ticket + `escrow_fingerprint` (open escrow for claimed budget/runs). Set the
  same `HACKME_ORDERS_PREPAID_HUB_SECRET` on hub + Orders (≠ publish/admin/worker),
  stored as **`pool.orders.prepaid_hub.secret`** (never reuse `pool.orders.token`).
  Prefer hub-only key: omit the prepaid secret from customer Orders, set
  `HACKME_ORDERS_PREPAID_LOCAL_SIGN=0` (default when `MINTING_HUB`/`REQUIRE_HUB` is set),
  and mint via admin `POST /api/fuzz/pool/prepaid-ticket` so a leaked fleet publish
  token cannot forge `hubv1:` tickets. Lab (no minting hub) still accepts self-MAC.
  Settle ACK needs origin Apply receipts (MAC over event_id+campaign+**hub outbox-bound**
  `units_hash`), not bare `confirm_applied`. Hub verifies `units_hash` against outbox
  fields (kind/miner/severity/work_item/outcome) so false ACKs cannot invent escrow
  unit vectors (PUB-07). Settle-pull only **applies** rows for local escrow; do not
  ACK foreign campaigns. Per-publish owner secret is stored on Orders as a `0600` file
  under `HACKME_ORDERS_OWNER_SECRET_DIR` / `data/orders_owner_secrets/` (not plaintext in
  `config_json`).   Shared fleet publish token still needs rotation if leaked (PUB-09);
  per-owner `owner_ref` + secret already scopes ACK/list — full per-tenant tokens are a
  redesign, not an incremental harden. Owner claim is header-only
  (`X-Hackme-Orders-Owner-Ref`); query `owner_ref` is ignored. Publish-scoped reads
  require a stamped `orders_owner_secret_hash` (legacy owner_ref-only rows denied).
  Admin `hub_applied` ACK is refused for `orders_publish` outbox rows (receipts required).
  Publish Hunt harness upload requires `harness_content_sha256`. Publish harness **GET**
  body requires the same content attestation header (HEAD probe OK without it); workers
  fetch freely. Publish routes accept `X-Hackme-Admin-Token` / Bearer only — not
  `X-Hackme-Worker-Token` (AUTH-01).
- **Med-prepaid / minting hub:** publish-token self-MAC is lab-only. On D1
  (`HACKME_CHAIN_MINTING_HUB=1` or `HACKME_ORDERS_REQUIRE_HUB_PREPAID_TICKET=1`) the
  coordinator **refuses** self-MAC and requires a hub-signed ticket
  (`hubv1:<exp>:<mac>`) keyed by `HACKME_ORDERS_PREPAID_HUB_SECRET` (≠ publish/admin/worker)
  plus `escrow_fingerprint`, and **live** `GetFuzzEscrow` on
  `HACKME_COORDINATOR_ORDERS_URL` must show open matching budget/runs before mint or
  accept (PUB-03). Orders may local-sign only with the distinct prepaid hub secret and
  explicit `HACKME_ORDERS_PREPAID_LOCAL_SIGN=1` (default is hub-only under minting hub).
  Admin mint: `POST /api/fuzz/pool/prepaid-ticket`. Documented break-glass only:
  `HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT=1` skips the hub escrow RPC (ops / lab).
- Publish cannot: cancel/complete via status API **or** register upsert; settle replay;
  cleanup-stale/gates/repair-zombies; PoH attach (`attach_poh_order` stripped).
  Harness upload + HEAD verify are allowed; publish GET body needs content sha
  (Windows attach flow uses publish then `harness_hash` on create).
  Prefer **`POST /api/hunt/campaigns/one-shot`** with `binary_path` for custom inventory — publish+create fails early with
  `harness_too_large` / `publish_rate_limited` / `harness_sync_failed` instead of opaque HTML 413/502 mid-flight.
  Coordinator harness POST max body = **36 MiB** (`36<<20`).
- Publish cannot hijack existing non-`orders_publish` campaign IDs (HTTP 409).
- Settle outbox GET/ACK with publish **requires `campaign_id`** and only works for
  `orders_publish` campaigns (no global list / cross-tenant ACK of admin campaigns).
- Progress + findings mirror accept publish for `orders_publish` campaigns only.
- Publish IDs are ASCII `[A-Za-z0-9._-]` only (no `..`, no unicode lookalikes).
- Build env scrub drops `HACKME_*ORDERS_PUBLISH*` / coordinator worker+admin tokens.
- `pool.orders.token` must be mode ≤0600 (same as other secrets).

## Operator checklist

1. `bash scripts/ops/gen_orders_publish_token.sh`
2. `bash scripts/ops/gen_orders_prepaid_hub_secret.sh` — set on **hub / command node only**
   (`HACKME_ORDERS_PREPAID_HUB_SECRET`). Prefer omit from customer Orders
   (`HACKME_ORDERS_PREPAID_LOCAL_SIGN=0` / default under minting hub).
   **Do not ship** `pool.orders.prepaid_hub.secret` in customer rc20+ bundles (PUB-01).
3. Hub command node (`HACKME_CHAIN_MINTING_HUB=1`): prepaid hub secret + publish token;
   nginx allowlists `/api/orders/pool/*` and `/api/orders/fund*`; settle URL
   `HACKME_COORDINATOR_ORDERS_URL` → `http://127.0.0.1:18080`. Leave
   `HACKME_ORDERS_PREPAID_ALLOW_ALGEBRAIC_MINT` unset on prod.
4. Customer Orders release: `HACKME_RELEASE_ORDERS_PUBLISH_TOKEN=…`,
   `HACKME_ORDERS_HUB_URL=https://hackme.tech` (Fund confirm + pool Dig/Hunt),
   `HACKME_ORDERS_ALLOW_LOCAL_FALLBACK=0`, `LOCAL_SIGN=0`. No prepaid hub secret in the pack.
5. Smoke (rc20): customer Fund Orders → hub tenant prepaid → `POST /api/orders/pool/dig`
   (or local Dig UI with hub URL) → workers claim → settle on hub → cancel refunds prepaid.
6. **Windows Orders:** `start_hackme_orders.bat` seeds/repairs `HACKME_FUZZ_SETTLE_PULL=1` (miner `hackme.env` may seed `0`). Copy `pool.orders.token.example` → `pool.orders.token` with the scoped publish token. Hunt engines stay Linux-pool-only.
7. **Do not deploy** until unit tests + this checklist pass

### rc20 customer path (no admin mint)

1. Fund Orders (on-chain → DevFee, memo `orders_fund_v1:<addr>`) then hub
   `POST /api/orders/pool/fund/confirm` (publish token + `X-Hackme-Orders-Owner-Ref`;
   tx `from` must match Owner-Ref).
2. Dig/Hunt with `pool_distributed=true` → customer proxies to hub
   `/api/orders/pool/dig|hunt` with publish token + Owner-Ref + **Ed25519 owner proof**
   (`X-Hackme-Orders-Owner-PubKey` / `-Sig` / `-Ts`) so a leaked fleet token cannot spend
   another tenant's prepaid. Hub opens escrow from **tenant prepaid**, mints `hubv1`,
   registers on coordinator. Customer does **not** open local escrow.
3. Package price is fee-from-price (e.g. 60 = 54 escrow + 6 fee). Cancel/finalize unused
   escrow → Orders prepaid (not on-chain wallet).
4. Failed pool register returns `pool_unavailable` — no `local_fallback` unless
   `HACKME_ORDERS_ALLOW_LOCAL_FALLBACK=1` (lab only).

Local Dig/Hunt with `pool_distributed=false` (CI) still works with no publish token.
