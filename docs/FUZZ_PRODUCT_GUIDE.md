# Fuzz campaigns — operator guide

HackMe exposes **fuzz / property / symbolic** campaigns under **`/api/fuzz/campaigns`** with SQLite-backed findings, corpus, runtime samples, and customer-facing reports. Full HTTP tables live in **`docs/API.md`** (section “Fuzz Campaigns”).

---

## 1) Release gate (mandatory before shipping)

From repo root, against a running node:

```bash
ADMIN_TOKEN=… BASE=http://127.0.0.1:8080 bash scripts/ops/fuzz_release_gate.sh
```

Optional skips via env inside that script (`RUN_LANGUAGE_MATRIX`, etc.). For static WASM/lang checks first:

```bash
MODE=lang_static bash scripts/tests/run_daily.sh
```

---

## 2) Background autorunner

Started by the node when **`HACKME_FUZZ_AUTORUN`** is true (default **on**). Set **`HACKME_FUZZ_AUTORUN=0`** to disable server-side ticking.

| Env | Role |
|-----|------|
| `HACKME_FUZZ_AUTORUN_TICK_SEC` | Tick interval (1–60 seconds); default ~2s in code if unset |
| `HACKME_FUZZ_RETENTION_INTERVAL_SEC` | How often autorunner triggers bounded DB/file retention |
| `HACKME_FUZZ_ARTIFACT_DIR` | Filesystem root for fuzz artifacts |
| `HACKME_FUZZ_ARTIFACT_TTL_SEC` / `HACKME_FUZZ_ARTIFACT_MAX_BYTES` | Artifact cleanup policy |

Per-campaign opt-out: set **`config.auto_runner`** to **`false`** (`config` JSON on create or PATCH flows per API).

Worker/item tuning (defaults are clamped in code):

- `HACKME_FUZZ_WORK_MAX_ATTEMPTS`, `HACKME_FUZZ_WORK_LEASE_SEC`, `HACKME_FUZZ_WORK_TIMEOUT_MS`
- Retention caps: `HACKME_FUZZ_RETENTION_FINDINGS_PER_CAMPAIGN`, `…_CORPUS_…`, `…_RUNTIME_SAMPLES_…`

---

## 3) Create → run → report flow

1. **POST** `/api/fuzz/campaigns` (admin) — types `fuzz`, `property`, `symbolic`; returns **`customer_report_token`** once (store securely).
2. **POST** `/api/fuzz/campaigns/{id}/status` — move `planned` → `running` if not already started by autorunner/runtime posts.
3. **POST** `/api/fuzz/campaigns/{id}/runtime` — workers or integrations push progress (`runs_done`, coverage counters).
4. **POST** `/api/fuzz/campaigns/{id}/findings` — ingest deduped findings.
5. Customer reads **`GET`** `/api/fuzz/campaigns/{id}/report` with header **`X-Hackme-Report-Token: <token>`** (or admin token).

Minimal create body (matches smoke scripts):

```json
{
  "id": "campaign-smoke-001",
  "campaign_type": "fuzz",
  "status": "planned",
  "title": "smoke",
  "budget_runs": 10
}
```

Reference script: **`scripts/tests/fuzz_runtime_gate.sh`**.

---

## 4) CI / release gate endpoint

**GET** `/api/fuzz/campaigns/{id}/gate?max_critical=0&max_high=0&max_severity_score=0&min_sample_size=1&min_runs_done=50`

`sample_size` is the **finding count** in the report sample, not executions. Gate `pass` / CLEAN wording means thresholds were met — not “proven secure”. Use `min_runs_done` for real execution depth.

Returns **`pass`** plus **`reasons[]`** when thresholds are violated. Same auth as report (**report token** or **admin**).

**GET** `/api/fuzz/campaigns/{id}/pulse` — live progress for dashboards (token or admin).

---

## 5) Housekeeping

- **POST** `/api/fuzz/housekeeping` — global bounded sweep (admin).
- **POST** `/api/fuzz/campaigns/{id}/housekeeping` — per campaign.
- **POST** `/api/fuzz/artifacts/cleanup` — filesystem artifact cleanup (admin).

---

## 6) Operator checklist cross-links

See **`docs/OPERATOR_FINAL_CHECKLIST.md`** for predeploy order and **`docs/ULTIMATE_VALIDATION_RUNBOOK.md`** for exhaustive passes.
