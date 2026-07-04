# Verdicts & finals (GitHub / operators)

Factual pass/fail snapshots for releases and gates. **Not** social copy for Telegram or Bitcointalk.

| Document | What it records |
|----------|-----------------|
| [../HACKME_RC11R.md](../HACKME_RC11R.md) | **Current channel rc11r** — Linux layout, fuzz settle, Win/Linux/ISO |
| [../archive/rc/HACKME_RC11L.md](../archive/rc/HACKME_RC11L.md) | ISO channel rc11l — live USB boot fix (historical) |
| [../archive/rc/HACKME_RC11M.md](../archive/rc/HACKME_RC11M.md) | Historical rc11m — wallet treasury |
| [../archive/rc/HACKME_RC11K_LAUNCH_CANDIDATE.md](../archive/rc/HACKME_RC11K_LAUNCH_CANDIDATE.md) | rc11k launch notes (superseded) |
| [../archive/vast/VAST_GPU_MATRIX_VERDICT_20260602.md](../archive/vast/VAST_GPU_MATRIX_VERDICT_20260602.md) | GPU matrix field test (historical) |

Older RC notes: [../archive/rc/](../archive/rc/) · VAST GPU logs: [../archive/vast/](../archive/vast/)

## Do not treat as verdicts

- Social drafts (`TELEGRAM_POST_*`, `BITCOINTALK_*`) are **not** in the repo — publish via [../TELEGRAM_NEWS_BOT_RUNBOOK.md](../TELEGRAM_NEWS_BOT_RUNBOOK.md) and https://hackme.tech/news.html
- `web/site/assets/news.json` — public site + Telegram bot feed (marketing tone OK)

## Version policy

Win/Linux channel: `scripts/release/CURRENT_VERSION` → **0.1.0-rc11r**. ISO channel: `scripts/release/CURRENT_ISO_VERSION` → **0.1.0-rc11r**.
