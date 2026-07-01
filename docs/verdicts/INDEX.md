# Verdicts & finals (GitHub / operators)

Factual pass/fail snapshots for releases and gates. **Not** social copy for Telegram or Bitcointalk.

| Document | What it records |
|----------|-----------------|
| [../HACKME_RC11R.md](../HACKME_RC11R.md) | **Current channel rc11r** — Linux layout, fuzz settle, Win/Linux/ISO |
| [../HACKME_RC11M.md](../HACKME_RC11M.md) | Historical rc11m — wallet treasury |
| [../HACKME_RC11L.md](../HACKME_RC11L.md) | ISO channel rc11l — live USB boot fix, SHA |
| [../../reports/FINAL_VERDICT_RC11L.md](../../reports/FINAL_VERDICT_RC11L.md) | Operator GO/HOLD snapshot (2026-06-04) |
| [../VAST_GPU_MATRIX_VERDICT_20260602.md](../VAST_GPU_MATRIX_VERDICT_20260602.md) | GPU matrix field test (historical) |
| [../HACKME_RC11K_LAUNCH_CANDIDATE.md](../HACKME_RC11K_LAUNCH_CANDIDATE.md) | rc11k launch notes (superseded for ISO by rc11l) |

## Do not treat as verdicts

- Social drafts (`TELEGRAM_POST_*`, `BITCOINTALK_*`) are **not** in the repo — publish via [../TELEGRAM_NEWS_BOT_RUNBOOK.md](../TELEGRAM_NEWS_BOT_RUNBOOK.md) and https://hackme.tech/news.html
- `web/site/assets/news.json` — public site + Telegram bot feed (marketing tone OK)

## Version policy

Win/Linux channel: `scripts/release/CURRENT_VERSION` → **0.1.0-rc11r**. ISO channel: `scripts/release/CURRENT_ISO_VERSION` → **0.1.0-rc11r**.
