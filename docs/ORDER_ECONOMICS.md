# Order economics — pool solves + escrow to miner

## Pool-distributed security audits (`attach_poh_order`)

When a customer node creates Dig/Hunt with **`pool_distributed: true`** (product default) and `HACKME_ORDERS_HUB_URL` set (**rc20+**):

- Customer Orders is a **thin client**: Fund → hub tenant prepaid → `POST /api/orders/pool/dig|hunt`.
- **Fuzz escrow opens on the hub** from that prepaid (not on the laptop SQLite). Cancel/finalize refund unused escrow **to Orders prepaid**, not the on-chain wallet.
- Package prices are **fee-from-price** under Economics v2 (live from block **#550,000**): listed HMC includes 10% (5% burn + 5% platform). Example: Hunt Standard **60 = 54 miner escrow + 6 fee**.
- Hub mints `hubv1` and registers on the coordinator; settle applies on the hub node. See [ORDERS_PUBLISH.md](ORDERS_PUBLISH.md) §rc20.

When also **`create_poh_order: true`** (optional Dig attach):

- PoH escrow is **not** opened on the customer node (pool workers never see a remote SQLite task list).
- Campaign config carries `attach_poh_order` + Dig `wasm_check_hex`; coordinator creates the PoH task on **`HACKME_COORDINATOR_ORDERS_URL`**.
- **PoH gate WASM** defaults to embedded `upstream_hackme_order_gate`. Override with `poh_wasm_check_hex`, or lab `poh_use_campaign_wasm=true`.
- Deep fuzz **runs** use `/api/fuzz/work/*`. Hub **exec_per_unit** capped on distributed pool — [POOL_FUZZ_DISTRIBUTED.md](POOL_FUZZ_DISTRIBUTED.md).

**B2B Dig packages (wizard):** scan ~1 HMC/64 · audit ~5/256 · deep ~40 HMC **price**/2048 (**40/60** of post-fee escrow). Packs: `secrets`, `script_bounds`, `filter_utf8`, `parser_expat`.

**Hunt packages:** `hunt_lite` ~20 · `hunt_standard` ~60 · `hunt_heavy` ~150 HMC **price** · **50/50** of post-fee escrow — [HUNT_ECONOMICS.md](HUNT_ECONOMICS.md). Dig smoke **20/80**; Dig Deep **40/60**.

Disable PoH attach with `HACKME_COORDINATOR_ATTACH_POH_ORDER=0`. Local Dig/Hunt: `pool_distributed=false` (CI / lab).

`HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN` must match the command-node **`HACKME_ADMIN_TOKEN`**
(or another secret accepted by `requireDeveloperTasksAuth` / `requireAdminAuth`). A distinct
coordinator-only token yields HTTP 401 on `POST /api/tasks` and attach silently fails.

## Customer pays for volume (target_solves)

- **Prepaid** = `reward_hmc × target_solves` (held in `meta_order_escrow_units`). **Minimum prepaid = 0.05 HMC** (`MinOrderPrepaidHMC`) — prevents dust orders that pay miners negligible per-solve slices.
- **Platform fee** = 5% of prepaid at order open (to dev fee address).
- **Burn tally** = 10% of prepaid recorded in supply metadata at open.
- **Economics v2** (live from block #550,000): Dig/Hunt take **10% from package price**; PoH useful-work still prices fee **on top of** fixed escrow — [RFC_ECONOMICS_V2.md](RFC_ECONOMICS_V2.md).

This is **not** a single “100 HMC to whoever finds a critical bug” jackpot unless you design the manifest that way (e.g. `target_solves: 1` with a high `reward_hmc`).

## Who can commit an order block

1. **Pool worker** — coordinator `orders` mode includes `order_task_id` + `wasm_check_hex` in `POST /api/work/claim`.
2. Worker finds PoH hit → runs `sandbox.InvokeCheck` → `POST /api/work/submit` with `wasm_gate_pass: true`.
3. Coordinator relays `POST /api/poh/solve-order` on the chain node (admin token).
4. Chain credits **miner_address** from escrow (hybrid signer address).

**Local node** — same path if you run `mining/start` on the node where you created the order (`miner_address` = node id).

## Coordinator vs escrow (no double pay on success)

When `order_chain_solve` succeeds:

- **Escrow** pays the per-solve `reward_hmc` to the solver on-chain.
- Coordinator pays **attempt accrual only** (no `found_bonus` on top).

## Env (operator)

| Variable | Purpose |
|----------|---------|
| `HACKME_COORDINATOR_ORDERS_URL` | Chain base for tasks probe + solve relay (e.g. `http://127.0.0.1:18080`) |
| `HACKME_COORDINATOR_ORDERS_ADMIN_TOKEN` | Chain admin for `GET /api/tasks` (manifest + wasm) + `POST /api/poh/solve-order`. Falls back to `HACKME_COORDINATOR_CHAIN_ADMIN_TOKEN` or `HACKME_COORDINATOR_ADMIN_TOKEN`. |
| `HACKME_COORDINATOR_ORDERS_SOLVE_RELAY` | `1` (default when ORDERS_URL set) — enable relay |

## API

- `POST /api/poh/solve-order` (chain, admin) — body: `miner_address`, `found_nonce`, `target_mod`, `order_task_id`.

## Fair pool vs chain command node

Pool workers compete on equal terms (hybrid `miner_address` on `solve-order`). Lab / emergency command nodes with `HACKME_CHAIN_LEADER_LOCAL_POH=1` can still run local GPU PoH and, by default, pick open orders from SQLite **before** pool relay (operator wallet wins). **Public hub D1 staging** runs `LOCAL_POH=0`, so empty blocks and order finds come from the pool path.

For production where **only pool rigs** should earn order escrow (even if local PoH is briefly re-enabled), set on the command node:

| Variable | Purpose |
|----------|---------|
| `HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY=1` | Local miner skips open orders; coordinator `orders` mode + `POST /api/poh/solve-order` only |

**Pool order `target_mod`:** workers search with the coordinator pool difficulty (`target_mod` in claim). On `POST /api/poh/solve-order`, the **chain always re-validates against canonical chain `poh_target_mod`** (caller-supplied lease M is ignored for escrow credit). Pool M still gates worker search; chain M gates payout correctness.

### Baseline (empty) finds → settlement (RFC D1)

When `HACKME_POOL_BASELINE_RELAY=on` (also accepts `1` / `true`) and the coordinator has chain admin + `HACKME_COORDINATOR_ORDERS_URL`, a verified **non-order** `found=true` relays `POST /api/poh/solve-baseline`. The empty-block subsidy credits the **primary wallet** (settlement payer), not the worker `miner_address`. Modes: `off` (default in code) · `dry` (count would-mint) · `on` (**live on public hub** with `LOCAL_POH=0`). Coordinator refuses `on` while hub local PoH is still enabled. Orders still use `solve-order` → escrow to miner.


## Honest limits

- WASM gate is a **simplified check**, not full `bitcoind` fuzzing.
- “Critical bug” is **your analysis** of traps/logs; there is no automatic CVE severity payout unless you add a separate bounty product.
