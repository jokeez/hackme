# Heavy VPS #2 — checklist before purchase

Use this **after** `bash scripts/ops/hms_prelaunch_gate.sh` passes on your dev machine.

## Verdict (May 2026)

| Question | Answer |
|----------|--------|
| **Is the software ready to buy VPS #2?** | **Yes, for capacity** (disk, Stratum ingress, storage workers). Hub already runs HMS coordinator for metrics/market API. |
| **Is the lane “live for miners” without VPS #2?** | **No** — 0 storage workers, Stratum off on hub, no sealed epochs with payouts yet. |
| **Will stock Antminer work on VPS #2?** | **No** (connection only). Valid seal shares need `tools/hms_stratum_asic_sim` or a custom gateway — see [HMS_ASIC_PILOT.md](HMS_ASIC_PILOT.md). |
| **When to buy?** | ~1 week before public go-live (roadmap), or **now** if you want pilot disk + Stratum on a dedicated IP without loading the hub. |

## What hub already has (hackme.tech)

- `hackme-hms-coordinator` on loopback `:18082` (`HMS_STRATUM_ENABLE=0`)
- Public read: `/api/hms/pool/stats`, market pricing/stats/capacity/quote, `/api/hms/economics`
- On-chain HMS genesis (~105k treasury), mint enabled
- Economics v2: **0.5 HMS** base per sealed epoch (~12 HMS/day at 1h epochs)
- Full miner UI + market order flow on **local hackme-node** only

## What VPS #2 is for

1. **Terabytes** for `HMS_MARKET_STORAGE_ROOT` (B2B backup chunks)
2. **Public Stratum** `:3334` without exposing hub
3. **Storage workers** + CPU seal load off the hub
4. Optional **coordinator migration** (same SQLite DB — do not run two coordinators with empty DB)

## Recommended rollout

### Option A — workers only (safest first purchase)

1. Buy VPS (Amsterdam, ≥2 TB disk, 4+ vCPU).
2. Bootstrap user `hackme`, copy `hackme-node` binary optional (only if running storage worker on heavy).
3. Run storage worker with `HACKME_HMS_COORDINATOR_URL=http://<hub-private-ip>:18082` (firewall hub **18082** from heavy IP only).
4. Keep coordinator + chain on hub.

### Option B — coordinator on heavy (production)

1. Deploy: `HEAVY_SSH=… MIGRATE_DB_FROM_HUB=1 bash scripts/ops/deploy_hms_heavy_vps.sh`
2. On hub `.env.vps`: `HACKME_HMS_COORDINATOR_URL=http://<heavy-private-ip>:18082`
3. `systemctl restart hackme-node`
4. Open **3334/tcp** on heavy; restrict **18082** to hub.

Env template: `scripts/ops/env.hms.heavy.example`

## Before announcing “HMS live”

- [ ] `hms_prelaunch_gate.sh` PASS
- [ ] ≥1 storage worker online (pool stats)
- [ ] ≥1 sealed epoch + `hms_epoch_settle.sh` mint (timer: `hackme-hms-epoch-settle.timer`)
- [ ] Market roundtrip on local node (pay HMC → upload → restore)
- [ ] MPS row for HMS under HackMe Official Pool
- [ ] ASIC path documented (sim or gateway), not “point Antminer at pool”

## Ops commands

```bash
# Local gate
bash scripts/ops/hms_prelaunch_gate.sh

# Heavy bootstrap
HEAVY_SSH=root@your-heavy bash scripts/ops/deploy_hms_heavy_vps.sh

# Hub epoch mint (cron/timer)
bash scripts/ops/hms_epoch_settle.sh

# Stratum smoke (heavy or local)
bash scripts/ops/hms_asic_pilot.sh
go run ./tools/hms_stratum_asic_sim -addr <host>:3334 -worker asic-sim-1
```
