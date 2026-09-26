# Hunt daily rotate (local FounderB lab)

Hourly ASAN Hunt on one OSS catalog target from `upstream/oss_cve_targets.json` rotation queue.
Covers ~24 libraries/day (queue wraps). Watch `reports/hunt-daily/YYYYMMDD/ROLLUP.md`.

## Quick start

```bash
# preview today's schedule + this hour's target
DRY_RUN=1 bash scripts/ops/hunt_daily_rotate.sh

# run one slot now (~1h wall, or shorter)
SLOT_WALL_SEC=900 bash scripts/ops/hunt_daily_rotate.sh

# install systemd --user hourly timer
bash scripts/ops/install_hunt_daily_rotate_user.sh
```

## What to watch

| Signal | Meaning |
|--------|---------|
| `family_count` / signature union | Depth that matters (honesty 2.0) |
| `CLEAN` streak | Honest quiet outcomes |
| `exec_per_sec` drift | Engine / machine regression |
| build failures | Target dropped from pack — fix or remove from queue |

## Layout

```
reports/hunt-daily/YYYYMMDD/
  schedule.json          # 24h plan
  rotate.log
  ROLLUP.md / ROLLUP.json
  HH00-<target>/
    hunt-local.json
    meta.json
    crashes/
```

`csonh` is skipped (disclosure hold). Single-flight via flock — won't stack soaks.
