# `scripts/ops` — operator scripts

Hundreds of one-shot and recurring shell helpers live here. Prefer **supported** paths below; treat the rest as experimental.

## Supported (production / miners)

| Script | Purpose |
|--------|---------|
| `run_pool_health_check.sh` | Pool API + difficulty snapshot |
| `public_site_smoke.sh` | Public site smoke (if present) |
| Settlement / SUP timers | Scripts referenced from systemd units + `docs/OPERATIONS_MONITORING.md` |

Always read the script header before running. Prefer dry-run / gate scripts over destructive ones.

## Experimental / lab

Prefixes often mean **lab-only** (not miner-facing defaults):

| Prefix / pattern | Notes |
|------------------|-------|
| `run_*` | Many are fine; still check for VPS/`rsync --delete` |
| `vps_*` | Operator VPS recovery / sync — **operator only** |
| `deploy_*` | Deploy helpers — confirm target; prefer dry-run |
| `mega_*` / `wow_*` | Stress / demo — not production defaults |
| `*_hms_*` / `hms_*` | HMS **prelaunch** — do not open publicly |

## Dangerous defaults

Scripts that deploy, recover VPS, or `rsync --delete` must stay **operator-only**. Prefer:

```bash
DRY_RUN=1 bash scripts/ops/<script>.sh
```

`prune_oss_cve_reports.sh` defaults to dry-run; use `APPLY=1` to delete.

## Inventory note

Do not mass-delete scripts without an inventory pass. Archive or move to `experimental/` only after confirming nothing in systemd / cron / docs references them.
