# Bootstrap customer bot

**Role:** independent «HackMe Bootstrap Audits» customer (not the main pool VPS).  
Deploy target defaults to `BOOTSTRAP_VPS` (see `deploy_bootstrap_vps.sh`).

**Listing disclosure:** this is operator-seeded Dig/Hunt demand on the public pool (same escrow APIs as any customer). Documented on [token-transparency.html#bootstrap-demand](https://hackme.tech/token-transparency.html#bootstrap-demand).

## How orders reach the public pool

Bootstrap places **`POST /api/security-audit`** locally with:

- `pool_distributed: true` — fuzz campaign → coordinator `/api/fuzz/*` (needs `workerfuzz` for deep runs/report progress)
- `create_poh_order: true` — **does not** escrow PoH on this VPS; config carries `attach_poh_order` so the **coordinator** creates the PoH task on the command chain (`ORDERS_URL`). Existing **`workerpoh`** fleet then auto-switches `baseline → orders`.

**PoH progress (`/api/tasks` `progress_count`) only moves on chain order solves** (find + WASM gate pass + `solve-order` relay). Leases/`runs_done` alone do not count.

**PoH WASM:** use `upstream_hackme_order_gate.wasm` (also embedded at `internal/sandbox/embed/`).  
Coordinator attach **ignores Dig `wasm_check_hex`** and uses the order gate unless `poh_wasm_check_hex` is set.  
Do **not** attach Dig `*_bounds_guard.wasm` / detector modules as the PoH gate — they reject almost all nonces (`0/N` stuck).  
Lab-only always-pass: `HACKME_ALLOW_MINIMAL_POH_GATE=1` (was `HACKME_MINIMAL_POH_GATE`).

No miner binary update for the PoH rail. Deep fuzz corpus work is still a separate claim path until `workerfuzz` eats fuzz.

## Wallet
- Address configured on the bootstrap VPS from its node seed (see `setup_bootstrap_vps.sh`)
- Bot spends **local fuzz escrow**; PoH attach prepaid is debited on the **command node** (`ORDERS_URL`) when the coordinator attach runs


## Live status (ops snapshot · 2026-10-12)

| Item | Value |
|------|--------|
| VPS | `root@89.150.41.40` · `/opt/hackme-bootstrap` |
| Node | `hackme-bootstrap-node` **active** · `0.1.0-rc18` |
| Dig bot | `hackme-bootstrap-bot.timer` · ~every 6h (next ~22:01 UTC) |
| Hunt bot | `hackme-bootstrap-hunt.timer` · ~every 6h (next ~22:10 UTC) |
| workerfuzz | `bootstrap-fuzz-01..04` **active** |
| Wallet | `HMC-23336d92da342724` |
| On-chain | ~**2851.67 HMC** · spendable for orders ~**1369 HMC** · ~**229 SUP** |
| Plan window | Dig/Hunt rotate until **2026-10-23** |
| Latest Dig | `campaign-bootstrap-cjson-20261012t150055z` · escrow **closed** · runs_paid ≈10 HMC · bounty refunded |

Timers show `inactive dead` between ticks — that is normal; only the node + workerfuzz stay running.

Settle-pull: HTTP `502` / connection resets through Cloudflare are retried; finalize waits and re-checks outbox so a fast Dig cannot close escrow before run rows land. If a premature close already left unpaid `run` rows stuck at the outbox head, recover with:

```bash
CAMPAIGN_ID=campaign-bootstrap-… bash /opt/hackme-bootstrap/scripts/bootstrap_customer/bootstrap_recover_stuck_settle.sh
```

## Services
| Service | Purpose |
|---------|---------|
| `hackme-bootstrap-node.service` | Customer node → `https://hackme.tech` canonical |
| `hackme-workerpoh.service` | CPU miner (unchanged) |
| `hackme-bootstrap-bot.timer` | New audit order every **6h** (~**4 orders/day**) · budgets **≥25 HMC** (Dig deep floor) · **max 5000 runs** (default **256**/timer; hard cap `MAX_BUDGET_RUNS`) · per-run ≥ **0.0001 HMC** · wall **8h** (`budget_seconds=28800`) |
| `hackme-bootstrap-workerfuzz@*` | Optional local **workerfuzz** fleet (3–10) claiming coordinator `/api/fuzz/work` |

## More dig capacity (fuzz)

`pool_distributed` campaigns are executed by **workerfuzz** on the coordinator claim path (not GPU `workerpoh`). To increase dig:

```bash
# on bootstrap VPS (preferred for customer demo — unique IDs + derived seeds)
WORKERFUZZ_COUNT=4 bash /opt/hackme-bootstrap/scripts/bootstrap_customer/workerfuzz_fleet.sh install
bash /opt/hackme-bootstrap/scripts/bootstrap_customer/workerfuzz_fleet.sh status
```

- Each unit needs a unique `WORKER_ID` and a unique miner seed (fleet derives SHA256 children from the node seed).
- Hub canary workerfuzz can remain at 1–2 instances; prefer scaling on the B2B VPS first so hub memory stays gentle.
- Escrow still settles on the **customer node**; PoH attach stays on the command chain.

## Targets (rotation)
`nghttp2` → `md4c` → `cjson` → `jsmn` → `yyjson` → `expat` → …

## Logs & snapshots
```
/opt/hackme-bootstrap/logs/bootstrap/bot.log
/opt/hackme-bootstrap/logs/bootstrap/snapshots/*/SUMMARY.json
/opt/hackme-bootstrap/logs/bootstrap/orders/
```

Each order captures:
- wallet before/after
- fleet GH/s + worker ranking (kapa-pc, desktop-stock01, …)
- pool fuzz stats + coordinator `scheduler_mode` / `orders_active`
- campaign progress polls
- escrow + report at end

## Manual
```bash
# one order now
bash /opt/hackme-bootstrap/scripts/bootstrap_customer/bootstrap_bot.sh

# resync stuck pool campaign
CAMPAIGN_ID=campaign-... bash /opt/hackme-bootstrap/scripts/bootstrap_customer/bootstrap_resync_pool.sh

# snapshot only
bash /opt/hackme-bootstrap/scripts/bootstrap_customer/bootstrap_snapshot.sh $(date -u +%Y%m%dT%H%M%SZ) manual
```

## First live order
- `campaign-bootstrap-nghttp2-20260711t164829z`
- 12 HMC · 512 runs · bytes_corpus deep pool
- Synced to production coordinator ✅

**Posts / site publish:** manual (not automated).
