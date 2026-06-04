# Verdicts & finals (GitHub / operators)

Factual pass/fail snapshots for releases and gates. **Not** social copy for Telegram or Bitcointalk.

| Document | What it records |
|----------|-----------------|
| [../HACKME_RC11L.md](../HACKME_RC11L.md) | Current channel rc11l — ISO boot fix, SHA, hold criteria |
| [../../reports/FINAL_VERDICT_RC11L.md](../../reports/FINAL_VERDICT_RC11L.md) | Operator GO/HOLD snapshot (2026-06-04) |
| [../VAST_GPU_MATRIX_VERDICT_20260602.md](../VAST_GPU_MATRIX_VERDICT_20260602.md) | GPU matrix field test (historical) |
| [../HACKME_RC11K_LAUNCH_CANDIDATE.md](../HACKME_RC11K_LAUNCH_CANDIDATE.md) | rc11k launch notes (superseded for ISO by rc11l) |

## Do not treat as verdicts

- `docs/TELEGRAM_POST_*` — channel drafts (see [../archive/telegram/README.md](../archive/telegram/README.md))
- `docs/BITCOINTALK_*_BBCode.txt` — forum paste templates
- `web/site/assets/news.json` — public site + Telegram bot feed (marketing tone OK)

## Version policy

Single channel: `scripts/release/CURRENT_VERSION` → **0.1.0-rc11l**. No suffix after `l` until USB boot verified on hardware.
