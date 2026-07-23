# Ultimate Validation Runbook (Single Massive Pass)

One command path to run the most comprehensive checks without splitting by day.

## What it executes

1. Health endpoints (`/api/status`, `/api/metrics`)
2. Full daily gate (`run_daily.sh` in `MODE=full`)
3. Unit integrity gate (`go test ./internal/chain ./internal/block`) — optional
4. Adversarial matrices (transfers/orders/security/coordinator + summary) - partially coincides with step 2; for long runs: `SKIP_ADV_MATRIX_BUNDLE=1`
5. Mega stress harness (high concurrent load + post-security assertions)
6. Pre-release pass (includes soak)
7. Optional multi-node rehearsal

It writes an ultimate summary and exits non-zero on any failed stage.

## Main command (aggressive default)

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=ultimate_main_01 \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
MEGA_DURATION_SEC=1800 \
MEGA_TX_WORKERS=48 \
MEGA_ORDERS_WORKERS=16 \
MEGA_COORD_WORKERS=24 \
PRE_DURATION_SEC=3600 \
PRE_INTERVAL_SEC=120 \
scripts/tests/run_ultimate_validation.sh
```

## Extreme profile (allowed to destabilize node)

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=ultimate_extreme_01 \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
MEGA_DURATION_SEC=3600 \
MEGA_TX_WORKERS=96 \
MEGA_ORDERS_WORKERS=32 \
MEGA_COORD_WORKERS=48 \
MEGA_SAMPLE_INTERVAL_SEC=1 \
PRE_DURATION_SEC=7200 \
PRE_INTERVAL_SEC=120 \
scripts/tests/run_ultimate_validation.sh
```

## Optional rehearsal mode

```bash
RUN_REHEARSAL=1 \
NODES="http://192.168.1.113:8080,http://192.168.1.114:8080" \
RUN_ID=ultimate_rehearsal_01 \
scripts/tests/run_ultimate_validation.sh
```

## Outputs

- `reports/tests/<RUN_ID>/ultimate/results.jsonl`
- `reports/tests/<RUN_ID>/ultimate/summary.json`
- `reports/tests/<RUN_ID>/summary_all.json`

Useful phase outputs:

- `reports/tests/<RUN_ID>_full/summary_all.json`
- `reports/tests/<RUN_ID>_adv/summary_all.json`
- `reports/tests/<RUN_ID>_mega/mega_stress/stress_report.json`
- `reports/tests/<RUN_ID>_pre/summary_all.json`

Release gate notes:

- `scripts/tests/release_readiness_gate.sh` now requires valid `summary_all.json` artifacts (`suites >= 1`, `total_cases >= 1`) for all referenced gates.
- For the `pre` gate, soak suite artifact must be present (`.../soak/summary.json`).

## Recovery quick path (if node crashes)

```bash
cd /home/kapa/Desktop/HackMe
go run .
MODE=quick RUN_ID=recover_quick BASE=http://127.0.0.1:8080 scripts/tests/run_daily.sh
```

