# Customer deliverables — fuzz campaigns (v1)

What an audit customer should receive after a campaign completes.

## Standard package

| Item | How |
|------|-----|
| **Campaign summary** | `GET /api/fuzz/campaigns/{id}` (admin) or dashboard export when exposed. |
| **Unified security report** | `GET /api/fuzz/campaigns/{id}/report` with **`X-Hackme-Report-Token`** issued at creation or after **`POST …/token`**. Schema **`fuzz_report_v1`**. |
| **Executive CSV** | `GET …/report.csv` with same token. |
| **CI verdict** | `GET …/gate?max_critical=…` — machine-readable pass/fail. |
| **Progress pulse** | `GET …/pulse` — optional live snapshot during **`running`**. |

## SLA-style norms (operator-facing)

- Deliver **`customer_report_token`** through a secure channel; treat like a password.
- Rotate token after accidental disclosure (`POST …/token`).
- Align acceptance with **`gate`** thresholds before marking **`completed`**.
- Attach **`reports/tests/*/fuzz_*`** directories from internal gates when responding to enterprise diligence requests.
