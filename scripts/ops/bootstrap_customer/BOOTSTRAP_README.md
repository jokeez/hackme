# Bootstrap customer bot

**Role:** independent «HackMe Bootstrap Audits» customer (not the main pool VPS).  
Deploy target defaults to `BOOTSTRAP_VPS` (see `deploy_bootstrap_vps.sh`).

## How orders reach the public pool

Bootstrap places **`POST /api/security-audit`** locally with:

- `pool_distributed: true` — fuzz campaign → coordinator `/api/fuzz/*` (needs `workerfuzz` for deep runs/report progress)
- `create_poh_order: true` — **does not** escrow PoH on this VPS; config carries `attach_poh_order` so the **coordinator** creates the PoH task on the command chain (`ORDERS_URL`). Existing **`workerpoh`** fleet then auto-switches `baseline → orders`.

No miner binary update for the PoH rail. Deep fuzz corpus work is still a separate claim path until `workerpoh` also eats fuzz.

## Wallet
- Address configured on the bootstrap VPS from its node seed (see `setup_bootstrap_vps.sh`)
- Bot spends **local fuzz escrow**; PoH attach prepaid is debited on the **command node** (`ORDERS_URL`) when the coordinator attach runs

## Services
| Service | Purpose |
|---------|---------|
| `hackme-bootstrap-node.service` | Customer node → `https://hackme.tech` canonical |
| `hackme-workerpoh.service` | CPU miner (unchanged) |
| `hackme-bootstrap-bot.timer` | New audit order every **36h** |

## Targets (rotation)
`nghttp2` → `md4c` → `cjson` → …

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
