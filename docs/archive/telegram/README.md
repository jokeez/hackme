# Telegram post drafts (not GitHub “verdicts”)

Files named `docs/TELEGRAM_POST_*` are **operator paste templates** for manual or one-off sends. They are kept for history but are **not** release verdicts.

**Canonical automated channel posts** come from:

- `web/site/assets/news.json` → `news-feed.json` (top 12 items)
- `scripts/ops/telegram/news_channel_bot.py` (HTML formatting, buttons)

Optional per-item copy: `"telegram": { "headline", "lead", "bullets", "footer" }` in `news.json`.

Do not duplicate long Telegram prose into `reports/` or commit social threads as if they were test verdicts.
