# Long network checks (soak) - how to “feel” whether the stack is holding

Goal: not a one-time `curl`, but a **timeline** - HTTP errors, delays, stagnation `tip_height`, coordinator availability.

## Tool

```bash
# Default: 30 min, every 30 s, BASE=https://hackme.tech
bash scripts/ops/network_stability_soak.sh

# Explicit: 2 hours, every 60 s, + light coordinator poll
BASE=https://hackme.tech \
COORD_URL=https://hackme.tech/pool/coordinator \
DURATION_SEC=7200 INTERVAL_SEC=60 \
bash scripts/ops/network_stability_soak.sh
```

Report: `reports/soak-<RUN_ID>/events.jsonl` (line-by-line JSON) and `summary.txt`.

## Phases (recommended timing)

| Phase | Duration | What to watch |
|------|----------------|--------------|
| **A. Fast regression** | already in repo | `bash scripts/ops/repo_final_selfcheck.sh` (if necessary `RUN_LOCAL_STACK_SMOKE=1`, `PUBLIC_RO_BASE=…`) |
| **B. Public soak** | 30–60 min | `summary.txt`: `status_http_fail` ≈ 0, `latency_ms_max` does not grow indefinitely, there is no spam in `events.jsonl` `tip_regressed` |
| **C. Day run** | 4–8 h | The same + manually a couple of times `GET /api/worker/settlement` and a dashboard; no growth of stuck curl on VPS (`ps` / `htop`) |
| **D. Night/Weekend** | 24–72 h | For a product release; compare first and last hour jsonl (jq), alerts by `status_fail` |

## How to interpret

- **`tip_height` does not grow for a long time** - normal if **local PoH on the command node is **off**; see `mining` and canonical tip in `global_metrics.chain` in follower mode.
- **`tip_regressed`** in the soak log - rare red flag (local/canon failure or circuit change); deal with `policy_hash`, P2P, backups.
- **`work_stats_fail`** — coordinator or nginx before it; check `systemctl`, limits, recent deployments.
- **Local “swarm” of workers** (load on coordinator, not public DNS):
  `DEMO_SEC=120 WORKER_COUNT=8 bash scripts/ops/simulate_pool_swarm_local.sh`

## Connection with “is the network holding”

Does the network hold = **stable percentage of 200**, **predictable latency**, **no degradation** beyond window B–D. The script does not replace host monitoring (RAM, FD, nginx), but provides a **reproducible** artifact for comparison before/after release.
