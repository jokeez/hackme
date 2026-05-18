# Mega Stress Test (Crash-Tolerant Stage)

This stage intentionally pushes node/coordinator hard, including overload scenarios.
Goal: find failure boundaries and recovery behavior with artifacts.

## Command

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=mega01 BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081 \
DURATION_SEC=1800 TX_WORKERS=32 ORDERS_WORKERS=10 COORD_WORKERS=16 \
scripts/tests/mega_stress.sh
```

## Outputs

- `reports/tests/<RUN_ID>/mega_stress/stress_report.json`
- `reports/tests/<RUN_ID>/mega_stress/summary.json`
- Global summary: `reports/tests/<RUN_ID>/summary_all.json`

## What it does

1. Optional precheck full gate (`PRECHECK_FULL=1` by default).
2. Parallel load bursts for:
   - `/api/tx/send` malformed flood (validation/rate-limit path)
   - `/api/tasks` order flood
   - `/api/work/claim` coordinator flood
3. Metrics sampling every `SAMPLE_INTERVAL_SEC`.
4. Optional postcheck security assertions (`POSTCHECK_SECURITY=1` by default).
5. PASS/FAIL decision from conservative thresholds.

## Useful knobs

- `PRECHECK_FULL=0` skip initial full suite
- `POSTCHECK_SECURITY=0` skip final security check
- `TX_WORKERS`, `ORDERS_WORKERS`, `COORD_WORKERS` tune pressure
- `DURATION_SEC` increase to 3600+ for sustained stress

## Notes

- Coordinator 405/unsupported mode is already tolerated by existing coordinator scripts.
- This test is designed to be aggressive; node degradation is possible by intent.
- If node crashes, restart node, collect reports, and run security assertions once again.

