# Memory & leak spec (coordinator)

Continuous churn test for coordinator heap stability and `ipAbuse` / `active_rigs` map growth.

## Run

```bash
# CI / dev (~3 min, 80 workers)
LEAK_SPEC_QUICK=1 bash scripts/tests/coordinator_memory_leak_spec.sh

# Full spec (2 h, 500 workers)
bash scripts/tests/coordinator_memory_leak_spec.sh
```

## Metrics

- `GET /api/work/admin/memstats` — `runtime.ReadMemStats` + map sizes (`ip_abuse_entries`, `active_rigs`, …)
- `POST /api/work/admin/gc` — force GC, return before/after snapshot

## Pass criteria

After drain + `clear-abuse` + GC, `heap_alloc_mb` ≤ measured baseline + `MARGIN_MB` (default 12).  
`ip_abuse_entries` and `abuse_workers` must not grow without bound (> workers + 50).

Report: `reports/tests/<run>/coordinator_memory_leak_spec/MEMORY_LEAK_SPEC_REPORT.md`
