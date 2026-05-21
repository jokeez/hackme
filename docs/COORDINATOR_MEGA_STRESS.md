# Coordinator mega stress test

Validates coordinator claim/submit under extreme concurrency, lease-boundary races, halving math, memory stability, and chaos/malformed ingress.

## Run

```bash
# Full spec (~10 min, 100 workers, ~25 req/s each target)
bash scripts/tests/coordinator_mega_stress.sh

# Quick smoke (~90 s, 50 workers)
STRESS_QUICK=1 bash scripts/tests/coordinator_mega_stress.sh
```

Reports: `reports/coordinator-mega-stress-LATEST/MEGA_STRESS_REPORT.md`

## What it does

1. Starts a **local** coordinator on `127.0.0.1:8082` with stress env (`scripts/tests/coordinator_stress.env`) — high rate caps, isolated SQLite DB.
2. Spawns N virtual workers (Python) targeting claim/submit at configurable RPS.
3. Injects a **30s lease boundary** synchronized submit burst (race).
4. Kills half the workers mid-flight (chaos).
5. Sends **1000 malformed** bodies (expects fast 400/409, no slow parse).
6. Samples coordinator **RSS every 1s** via `/proc`.
7. Runs Go halving tests (`internal/chain`) in parallel.

## Production note

Public pool (`hackme.tech`) uses **production rate limits** (~120 claim/min global). This suite is meant for the **local stress coordinator**, not against production at 2500 req/s.

## WASM

The coordinator process has **no WASM runtime**. Memory growth, if any, is in Go structures (`workManager.active`, dedup maps) — see report leak hint.
