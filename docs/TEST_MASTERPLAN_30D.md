# HackMe 30-Day Test Masterplan (Execution Runbook)

This runbook operationalizes the 30-day hardening plan with semi-automation.

## 1. Preconditions

- Node is running and healthy (`/api/status` returns JSON).
- `jq`, `curl`, `python3` are installed.
- Optional admin token exported for protected POST endpoints:
  - `export ADMIN_TOKEN="..."`

## 2. Run Artifacts

All outputs are written to:

- `reports/tests/<RUN_ID>/...`

Each suite writes:

- `results.jsonl`
- `summary.json`

Global summary:

- `summary_all.json`

## 3. Core Scripts

- `scripts/tests/baseline_snapshot.sh`
- `scripts/tests/transfers_matrix.sh`
- `scripts/tests/orders_matrix.sh`
- `scripts/tests/coordinator_matrix.sh`
- `scripts/tests/security_assertions.sh`
- `scripts/tests/soak_capture.sh`
- `scripts/tests/rehearsal_onboarding.sh`
- `scripts/tests/report_summary.sh`
- `scripts/tests/run_test_pack.sh`

## 4. Daily Cadence (30 days)

### Day 1-5: Baseline and smoke

```bash
RUN_ID=day01 BASE=http://127.0.0.1:8080 scripts/tests/baseline_snapshot.sh
RUN_ID=day01 BASE=http://127.0.0.1:8080 scripts/tests/security_assertions.sh
RUN_ID=day01 scripts/tests/report_summary.sh
```

PASS gate:

- no suite fail
- schema consistent
- economics invariants hold

### Day 6-10: Transfers + orders

```bash
RUN_ID=day06 BASE=http://127.0.0.1:8080 scripts/tests/transfers_matrix.sh
RUN_ID=day06 BASE=http://127.0.0.1:8080 ADMIN_TOKEN="$ADMIN_TOKEN" scripts/tests/orders_matrix.sh
RUN_ID=day06 scripts/tests/report_summary.sh
```

PASS gate:

- negative transfer cases rejected
- fairness guard rejects low reward orders

### Day 11-15: Coordinator + multi-PC

```bash
RUN_ID=day11 COORD=http://127.0.0.1:8081 scripts/tests/coordinator_matrix.sh
RUN_ID=day11 scripts/tests/report_summary.sh
```

PASS gate:

- claim/submit flow stable
- conflict and anti-abuse paths produce expected HTTP/reason

### Day 16-20: Security assertions

```bash
RUN_ID=day16 BASE=http://127.0.0.1:8080 scripts/tests/security_assertions.sh
RUN_ID=day16 scripts/tests/report_summary.sh
```

PASS gate:

- economics invariant checks pass
- malformed tx rejected
- no unauthorized emission symptoms

### Day 21-25: Soak

```bash
RUN_ID=day21 BASE=http://127.0.0.1:8080 DURATION_SEC=86400 INTERVAL_SEC=300 scripts/tests/soak_capture.sh
RUN_ID=day21 scripts/tests/report_summary.sh
```

PASS gate:

- no critical crashes across soak window
- trend remains stable (target difficulty converges, no runaway)

### Day 26-30: Rehearsal

```bash
RUN_ID=day26 NODES="http://192.168.1.113:8080,http://192.168.1.114:8080" scripts/tests/rehearsal_onboarding.sh
RUN_ID=day26 scripts/tests/report_summary.sh
```

PASS gate:

- all rehearsal nodes pass status/metrics/wallet checks
- no failed onboarding steps

## 5. One-shot pack run

```bash
RUN_ID=smoke_pack BASE=http://127.0.0.1:8080 ADMIN_TOKEN="$ADMIN_TOKEN" RUN_COORDINATOR_MATRIX=1 scripts/tests/run_test_pack.sh
```

## 6. Daily report template

- Run ID
- Suites executed
- PASS/FAIL per suite
- P0/P1/P2 issues found
- Decision: GO / NO-GO to next stage
