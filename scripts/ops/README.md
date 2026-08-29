# `scripts/ops` — operator scripts

Hundreds of one-shot and recurring shell helpers live here. Prefer **supported** paths below; treat the rest as experimental.

## Supported (production / miners)

| Script | Purpose |
|--------|---------|
| `run_pool_health_check.sh` | Pool API + difficulty snapshot |
| `deploy_hackme_site.sh` | Rsync `web/site` to hub + nginx reload |
| `generate_sitemap.sh` | Regenerate `web/site/sitemap.xml` |
| Settlement / SUP timers | Scripts referenced from systemd units + `docs/OPERATIONS_MONITORING.md` |

Always read the script header before running. Prefer dry-run / gate scripts over destructive ones.

## Canonical lab entry points

| Script | Purpose |
|--------|---------|
| `run_bounty_autopilot.sh` | Daily multi-track bounty fuzz (systemd timer) |
| `run_bounty_overnight.sh` | Detached overnight marathon |
| `run_oss_cve_hunt.sh` | ASAN upstream mutation runner |
| `run_oss_cve_nightly.sh` | Nightly rotation (systemd) |
| `run_oss_cve_wave.sh` | Generic wave runner (`WAVE=` + JSON registry) |
| `start_test_named_fleet.sh` | Hybrid PoH+fuzz display fleet (preferred) |

Docs: [../../docs/BOUNTY_AUTOPILOT.md](../../docs/BOUNTY_AUTOPILOT.md) · [../../docs/OSS_CVE_HUNT.md](../../docs/OSS_CVE_HUNT.md)

## Deprecated forwards

| Script | Use instead |
|--------|-------------|
| `start_test_named_fuzz_fleet.sh` | `start_test_named_fleet.sh` |
| `start_local_fair_workers.sh` | `start_local_pool_display_rig.sh` |

## Experimental / lab

Prefixes often mean **lab-only** (not miner-facing defaults):

| Prefix / pattern | Notes |
|------------------|-------|
| `run_*` | Many are fine; still check for VPS/`rsync --delete` |
| `run_bounty_*` (except autopilot/overnight) | One-shot hunts — overlap with autopilot phases |
| `run_oss_cve_wave[0-9]*.sh` | Frozen target lists — prefer `run_oss_cve_wave.sh` |
| `run_oss_cve_watch*` / `away_libheif_*` | Closed OSS CVE Watch series — archive/repro only |
| `vps_*` | Operator VPS recovery / sync — **operator only** |
| `deploy_*` | Deploy helpers — confirm target; prefer dry-run |
| `mega_*` / `wow_*` | Stress / demo — not production defaults |
| `*_hms_*` / `hms_*` | HMS **prelaunch** — do not open publicly |

See [archive/README.md](archive/README.md) for consolidated one-shot scripts.

## Dangerous defaults

Scripts that deploy, recover VPS, or `rsync --delete` must stay **operator-only**. Prefer:

```bash
DRY_RUN=1 bash scripts/ops/<script>.sh
```

`prune_oss_cve_reports.sh` defaults to dry-run; use `APPLY=1` to delete.

## Local-only (gitignored)

Away-mode, week-ops journals, CVE marathon/autopublish, ideal/finale one-shots, and similar machine-local helpers stay on disk but are **not** tracked (see root `.gitignore`). They still work under local systemd; do not `git add -f` them.

## Inventory note

Do not mass-delete scripts without an inventory pass. Archive or move to `archive/` only after confirming nothing in systemd / cron / docs references them.
