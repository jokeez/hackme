# Order economics — pool solves + escrow to miner

## Customer pays for volume (target_solves)

- **Prepaid** = `reward_hmc × target_solves` (held in `meta_order_escrow_units`).
- **Platform fee** = 5% of prepaid at order open (to dev fee address).
- **Burn tally** = 10% of prepaid recorded in supply metadata at open.

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

Pool workers compete on equal terms (hybrid `miner_address` on `solve-order`). The **chain command node** (`HACKME_CHAIN_LEADER_LOCAL_POH=1`) can still run local GPU PoH and, by default, picks open orders from SQLite **before** pool relay — it usually wins order blocks to `nodeID` (operator wallet).

For production where **only pool rigs** should earn order escrow, set on the command node:

| Variable | Purpose |
|----------|---------|
| `HACKME_CHAIN_LEADER_ORDERS_VIA_POOL_ONLY=1` | Local miner skips open orders; coordinator `orders` mode + `POST /api/poh/solve-order` only |

Empty-chain blocks still use the internal task fallback on the leader.

**Pool order `target_mod`:** workers search with the coordinator pool difficulty (`target_mod` in claim), not the chain leader’s solo M. `POST /api/poh/solve-order` accepts that lease M when the PoH hit and WASM gate are valid.

## Honest limits

- WASM gate is a **simplified check**, not full `bitcoind` fuzzing.
- “Critical bug” is **your analysis** of traps/logs; there is no automatic CVE severity payout unless you add a separate bounty product.
