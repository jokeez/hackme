# Mega Stress Runbook

This runbook is for intentional high load, including conditions where the node may become unstable.

## 1) Safety notes

- Run on a disposable/backup-ready environment.
- Keep a terminal with node logs open.
- Do not run against public/internet-exposed endpoints.

## 2) Fast smoke (2-5 min)

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=mega_smoke_local \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
PROFILE=mixed \
ORDERS_MODE=nospend \
PRECHECK_FULL=0 \
POSTCHECK_SECURITY=0 \
DURATION_SEC=120 \
TX_WORKERS=12 \
ORDERS_WORKERS=4 \
COORD_WORKERS=4 \
scripts/tests/mega_stress.sh
```

Inspect:

```bash
jq '.' reports/tests/mega_smoke_local/mega_stress/stress_report.json
jq '.' reports/tests/mega_smoke_local/mega_stress/summary.json
jq '.' reports/tests/mega_smoke_local/summary_all.json
```

## 3) Mega run (aggressive)

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=mega_day01 \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
PROFILE=tx-heavy \
ORDERS_MODE=nospend \
PRECHECK_FULL=1 \
POSTCHECK_SECURITY=1 \
DURATION_SEC=1800 \
TX_WORKERS=48 \
ORDERS_WORKERS=16 \
COORD_WORKERS=24 \
SAMPLE_INTERVAL_SEC=2 \
scripts/tests/mega_stress.sh
```

## 4) Extreme run (may crash node)

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=mega_extreme01 \
BASE=http://127.0.0.1:8080 \
COORD=http://127.0.0.1:8081 \
PROFILE=coord-heavy \
ORDERS_MODE=nospend \
PRECHECK_FULL=0 \
POSTCHECK_SECURITY=1 \
DURATION_SEC=3600 \
TX_WORKERS=96 \
ORDERS_WORKERS=32 \
COORD_WORKERS=48 \
SAMPLE_INTERVAL_SEC=1 \
scripts/tests/mega_stress.sh
```

## 5) Outputs

- `reports/tests/<RUN_ID>/mega_stress/stress_report.json`
- `reports/tests/<RUN_ID>/mega_stress/summary.json`
- `reports/tests/<RUN_ID>/summary_all.json`

Key indicators:

- `ratio_5xx` per scenario
- `ratio_network_error` per scenario
- metrics sampling quality (`samples`, `errors`)
- `min_hashrate_th_s` (detects hashrate collapse)
- post-security assertions status

Order spending behavior:

- `ORDERS_MODE=nospend` (default): sends invalid-fairness orders to stress API without debiting wallet escrow.
- `ORDERS_MODE=spend`: creates valid paid orders and can drain wallet quickly.
- `PROFILE` tunes load mix automatically:
  - `mixed` (default): balanced traffic
  - `tx-heavy`: emphasizes transfer API flood
  - `orders-heavy`: emphasizes order creation pressure
  - `coord-heavy`: emphasizes coordinator claim pressure

## 6) Recovery checklist after crash

1. Restart node: `go run .`
2. Check health:
   - `curl -fsS http://127.0.0.1:8080/api/status | jq '.has_genesis, .mining'`
3. Re-run a short gate:
   - `MODE=quick RUN_ID=recover_quick BASE=http://127.0.0.1:8080 scripts/tests/run_daily.sh`
4. Compare economics invariants before/after in reports.

