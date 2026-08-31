# `scripts/ops` — operator scripts (public repo)

Production and CI-supported helpers only. **Lab / one-shot / marathon scripts stay on your disk** under the same paths but are listed in [`../.gitignore`](../.gitignore) and are **not** pushed to GitHub.

## Supported (production / miners)

| Script | Purpose |
|--------|---------|
| `run_pool_health_check.sh` | Pool API + difficulty snapshot |
| `deploy_hackme_site.sh` | Rsync `web/site` to hub + nginx reload |
| `deploy_hackme_node.sh` | Hub node/coordinator deploy |
| `generate_sitemap.sh` | Regenerate `web/site/sitemap.xml` |
| `update_hackme_miner.sh` | Self-update miner (Linux/Win/OS) |
| `download_hackme_release.sh` | Fetch release tarball from hackme.tech |
| Settlement / SUP timers | Scripts in `systemd/` + `docs/OPERATIONS_MONITORING.md` |

Always read the script header before running. Prefer dry-run / gate scripts over destructive ones.

## Canonical lab entry points (tracked)

| Script | Purpose |
|--------|---------|
| `run_bounty_autopilot.sh` | Daily multi-track bounty fuzz (systemd timer) |
| `run_bounty_overnight.sh` | Detached overnight marathon |
| `run_oss_cve_hunt.sh` | ASAN upstream mutation runner |
| `run_oss_cve_nightly.sh` | Nightly rotation (systemd) |
| `run_oss_cve_wave.sh` | Generic wave runner (`WAVE=` + JSON registry) |
| `start_test_named_fleet.sh` | Hybrid PoH+fuzz display fleet (preferred) |
| `pool_fuzz_health_fix.sh` | Hub outbox drain + fuzz queue cleanup |

Docs: [../../docs/BOUNTY_AUTOPILOT.md](../../docs/BOUNTY_AUTOPILOT.md) · [../../docs/OSS_CVE_HUNT.md](../../docs/OSS_CVE_HUNT.md)

## Deprecated forwards

| Script | Use instead |
|--------|-------------|
| `start_test_named_fuzz_fleet.sh` | `start_test_named_fleet.sh` |
| `start_local_fair_workers.sh` | `start_local_pool_display_rig.sh` |

## Local-only lab (not on GitHub)

Historical marathons, OSS CVE Watch day scripts, Vast.ai matrix, mega-gates, VPS one-shots, and duplicate bounty hunts remain in your checkout but are **gitignored**. To run them locally: keep your tree, never `git add -f` those paths.

See [archive/README.md](archive/README.md) for naming patterns of retired one-shots.

## Dangerous defaults

Scripts that deploy, recover VPS, or `rsync --delete` must stay **operator-only**. Prefer:

```bash
DRY_RUN=1 bash scripts/ops/<script>.sh
```

`prune_oss_cve_reports.sh` defaults to dry-run; use `APPLY=1` to delete.
