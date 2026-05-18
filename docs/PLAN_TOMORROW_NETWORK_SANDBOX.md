# Tomorrow Runbook: Network Sandbox + Real Security Tests

This is the exact execution plan for tomorrow.
Follow in order, without mixing commands in one line.

---

## 0) Goal of the day

- Prove stability in sandbox network conditions.
- Run adversarial tests (not only happy-path).
- Decide GO / NO-GO for wider private expansion.

Decision is based on artifacts, not intuition.

---

## 1) Preconditions (10 min)

1. Ensure main node is running (`go run .`) and UI/API respond.
2. Keep coordinator state known:
   - If work API is unsupported on this build, coordinator suite may be skipped as PASS.
3. Ensure tools are installed: `jq`, `curl`, `python3`.

Quick checks:

```bash
curl -fsS http://127.0.0.1:8080/api/status | jq '.mining, .has_genesis'
curl -fsS http://127.0.0.1:8080/api/metrics | jq '.block_height, .mining_running'
```

---

## 2) Baseline full gate (20 min)

Run with a single run id:

```bash
cd /home/kapa/Desktop/HackMe
MODE=full RUN_ID=day_tomorrow_full BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081 scripts/tests/run_daily.sh
jq '.' reports/tests/day_tomorrow_full/summary_all.json
```

Pass condition:

- `status == "PASS"`
- `total_fails == 0`

If FAIL:

- Stop and fix only failed suite(s), then rerun this section.

---

## 3) Unit integrity gate (10 min)

```bash
cd /home/kapa/Desktop/HackMe
go test ./internal/chain ./internal/block
```

Pass condition:

- command exits 0

If FAIL:

- fix test regressions before moving to soak/adversarial stage.

---

## 4) Real adversarial checks (30-45 min)

Run these as intentional negative/abuse probes:

```bash
cd /home/kapa/Desktop/HackMe
RUN_ID=day_tomorrow_adv BASE=http://127.0.0.1:8080 scripts/tests/transfers_matrix.sh
RUN_ID=day_tomorrow_adv BASE=http://127.0.0.1:8080 scripts/tests/orders_matrix.sh
RUN_ID=day_tomorrow_adv BASE=http://127.0.0.1:8080 scripts/tests/security_assertions.sh
RUN_ID=day_tomorrow_adv COORD=http://127.0.0.1:8081 scripts/tests/coordinator_matrix.sh
RUN_ID=day_tomorrow_adv scripts/tests/report_summary.sh
jq '.' reports/tests/day_tomorrow_adv/summary_all.json
```

Pass condition:

- adversarial summary is PASS
- economics invariants hold
- malformed tx/order scenarios are rejected as expected

---

## 5) Pre-release soak in sandbox (60 min)

```bash
cd /home/kapa/Desktop/HackMe
MODE=pre_release RUN_ID=day_tomorrow_pre BASE=http://127.0.0.1:8080 COORD=http://127.0.0.1:8081 DURATION_SEC=3600 INTERVAL_SEC=120 scripts/tests/run_daily.sh
jq '.' reports/tests/day_tomorrow_pre/summary_all.json
```

Pass condition:

- final summary PASS
- no critical instability in telemetry/logs:
  - no runaway CPU/GPU temperatures
  - no repeated economic invariant alarms
  - no persistent API errors

---

## 6) Final decision (15 min)

Collect three artifacts:

- `reports/tests/day_tomorrow_full/summary_all.json`
- `reports/tests/day_tomorrow_adv/summary_all.json`
- `reports/tests/day_tomorrow_pre/summary_all.json`

GO to wider private expansion only if all are PASS.

NO-GO if any stage fails; record root cause and rerun failed stage only.

---

## 7) Operator notes

- Do not paste multiple commands as one concatenated line.
- Use one command per line or join with `&&` only when intentional.
- Keep the same `BASE`/`COORD` during a stage to avoid mixed artifacts.

