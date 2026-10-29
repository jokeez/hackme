# `scripts/ops` — operator scripts (public repo)

Production and CI-supported helpers only. Lab / one-shot / marathon scripts live under `scripts/local/` on operator machines (`ops/`, `tests/`, `vast/`) — that tree is **gitignored** and is not on GitHub. Spent one-shots that used to live here are in [`../archive/ops/`](../archive/ops/).

## Supported (production / miners)

| Script | Purpose |
|--------|---------|
| `run_pool_health_check.sh` | Pool API + difficulty snapshot |
| `deploy_hackme_site.sh` | Rsync `web/site` to hub + nginx reload |
| `deploy_hackme_node.sh` | Hub node/coordinator deploy |
| `generate_sitemap.sh` | Regenerate `web/site/sitemap.xml` |
| `update_hackme_miner.sh` | Self-update miner (Linux/Win/OS) |
| `download_hackme_release.sh` | Fetch release tarball from hackme.tech |
| Settlement / SUP timers | Scripts in `systemd/` + `docs/internal/OPERATIONS_MONITORING.md` |

Always read the script header before running. Prefer dry-run / gate scripts over destructive ones.

## Canonical lab entry points (tracked)

| Script | Purpose |
|--------|---------|
| `run_bounty_autopilot.sh` | Daily multi-track bounty fuzz (systemd timer) |
| `run_bounty_overnight.sh` | Detached overnight marathon |
| `run_oss_cve_hunt.sh` | ASAN upstream mutation runner |
| `run_oss_cve_nightly.sh` | Nightly rotation (systemd) |
| `run_oss_cve_wave.sh` | Generic wave runner (`WAVE=` + JSON registry) |
| `pool_fuzz_health_fix.sh` | Hub outbox drain + fuzz queue cleanup |
| `d1_baseline_staging_gate.sh` | RFC D1 readiness / dry-soak observer (no live mint) |

Docs: [../../docs/internal/BOUNTY_AUTOPILOT.md](../../docs/internal/BOUNTY_AUTOPILOT.md) · [../../docs/OSS_CVE_HUNT.md](../../docs/OSS_CVE_HUNT.md)

## Deprecated forwards

| Script | Use instead |
|--------|-------------|
| `start_test_named_fuzz_fleet.sh` | local display fleet (`start_test_named_fleet.sh`, gitignored) |
| `start_local_fair_workers.sh` | `start_local_pool_display_rig.sh` |

## Local-only lab (not on GitHub)

Historical marathons, OSS CVE Watch day scripts, Vast.ai matrix, mega-gates, and VPS one-shots stay on the operator machine under gitignored `scripts/local/`. Never `git add -f` that tree.

Retired tracked one-shots: [`../archive/README.md`](../archive/README.md).

## Dangerous defaults

Scripts that deploy, recover VPS, or `rsync --delete` must stay **operator-only**. Prefer:

```bash
DRY_RUN=1 bash scripts/ops/<script>.sh
```

`prune_oss_cve_reports.sh` defaults to dry-run; use `APPLY=1` to delete.
