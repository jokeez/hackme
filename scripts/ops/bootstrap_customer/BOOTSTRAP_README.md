# Bootstrap customer bot

**Role:** independent «HackMe Bootstrap Audits» customer (not the main pool VPS).  
Deploy target defaults to `BOOTSTRAP_VPS` (see `deploy_bootstrap_vps.sh`).

## How orders reach the public pool

Bootstrap places **`POST /api/security-audit`** locally with:

- `pool_distributed: true` — fuzz campaign → coordinator `/api/fuzz/*` (needs `workerfuzz` for deep runs/report progress)
- `create_poh_order: true` — **does not** escrow PoH on this VPS; config carries `attach_poh_order` so the **coordinator** creates the PoH task on the command chain (`ORDERS_URL`). Existing **`workerpoh`** fleet then auto-switches `baseline → orders`.

**PoH progress (`/api/tasks` `progress_count`) only moves on chain order solves** (find + WASM gate pass + `solve-order` relay). Leases/`runs_done` alone do not count.

**PoH WASM:** use `upstream_hackme_order_gate.wasm` (solvable). Do **not** attach security `*_bounds_guard.wasm` as the PoH gate — it rejects almost all nonces and leaves orders stuck at `0/N` while the pool still shows `scheduler=orders` and leases. Override with `WASM_FILE=...` or `HACKME_MINIMAL_POH_GATE=1`.

No miner binary update for the PoH rail. Deep fuzz corpus work is still a separate claim path until `workerfuzz` eats fuzz.

## Wallet
- Address configured on the bootstrap VPS from its node seed (see `setup_bootstrap_vps.sh`)
- Bot spends **local fuzz escrow**; PoH attach prepaid is debited on the **command node** (`ORDERS_URL`) when the coordinator attach runs

## Services
| Service | Purpose |
|---------|---------|
| `hackme-bootstrap-node.service` | Customer node → `https://hackme.tech` canonical |
| `hackme-workerpoh.service` | CPU miner (unchanged) |
| `hackme-bootstrap-bot.timer` | New audit order every **6h** (~**4 orders/day**) · budgets ~6–12 HMC with runs capped so per-run ≥ **0.0001 HMC** (else HTTP 402) |
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
