# Customer deliverables — fuzz / security-audit campaigns

What an audit customer should receive after a campaign completes.

## Standard package

| Item | How |
|------|-----|
| **Human summary** | `GET …/report` HTML/JSON — `human_summary`: N runs · coverage · crash-class bugs · “no critical”. Detector spam is **not** in the top. |
| **CI gate (primary)** | `GET …/gate?max_critical=0&max_high=0&min_runs_done=…` — pass/fail + short reasons. Thresholds use **crash-class** counts only (`triage_policy=crash_first`). |
| **Verdict card** | Report field `verdict_card`: runs · crashes · critical · gate PASS/FAIL · money spent. |
| **Crash-first triage** | `top_issues[]` = crash/hang/ASan/memory only; detector/property → `coverage_noise[]` appendix. |
| **Assurance note** | Honest: not proven secure; none found of X at N runs. |
| **1-click repro** | Crash-class issues include `repro{input_sha256,input_hex,command,ready}` (gap flagged if incomplete). |
| **Target fingerprint** | `target_fingerprint.wasm_sha256` / `binary_sha256` so the artifact cannot be swapped post-pay. |
| **Baseline diff** | `baseline_diff` when `config.base_campaign_id` is set; otherwise clear stub. |
| **Progress pulse** | `GET …/pulse` — ETA + crash vs coverage-noise counts. |
| **Executive CSV** | `GET …/report.csv` with same token. |
| **Proof of Fuzz** (opt-in) | `wizard --public-proof` → `/proof/{id}` + badge (crash-gate only; secrets redacted). |

## Wizard / packs (B2B final)

| Item | Detail |
|------|--------|
| **CLI** | `hackme-fuzzing wizard --pack secrets\|script_bounds\|filter_utf8\|… --package scan\|audit\|deep` |
| **Packages** | scan ~1 HMC/64 · audit ~5/256 · deep ~25/2048 |
| **Pool Deep** | 512 exec/unit local; **64 cap** on distributed pool — coordinator replay anticheat |
| **coverage_kind** | `wasm_edge_bitmap` @ mem **8192** on instrumented guards (scheduling, not AFL) |

Auth: **`X-Hackme-Report-Token`** (issued once at create / `POST …/token`) or admin token.

## SLA-style norms (operator-facing)

- Deliver **`customer_report_token`** through a secure channel; treat like a password.
- Rotate token after accidental disclosure (`POST …/token`).
- Align acceptance with **`gate`** (crash-class) before marking **`completed`**.
- Prefer **`min_runs_done`** over finding-count alone.
- Attach **`reports/tests/*/fuzz_*`** directories from internal gates when responding to enterprise diligence requests.
