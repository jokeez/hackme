#!/usr/bin/env bash
# Preview Telegram channel HTML for a news id (no send).
#   bash scripts/ops/telegram/preview_channel_post.sh 2026-06-04-rc11l-iso-live-boot
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
ID="${1:-2026-06-04-rc11l-iso-live-boot}"
FEED="${2:-$ROOT/web/site/assets/news.json}"
export NEWS_MINER_HINT_LINE="${NEWS_MINER_HINT_LINE:-1}"
export NEWS_SHOW_GITHUB_BUTTON="${NEWS_SHOW_GITHUB_BUTTON:-0}"

python3 - <<PY
import json, sys
sys.path.insert(0, "$ROOT/scripts/ops/telegram")
import news_channel_bot as nb

data = json.load(open("$FEED", encoding="utf-8"))
fid = "$ID"
item = next((x for x in data.get("items", []) if x.get("id") == fid), None)
if not item:
    raise SystemExit(f"id not found: {fid}")

msg = nb.render_message(item, miner_hint=bool(int("${NEWS_MINER_HINT_LINE:-1}")))
btn = nb.build_reply_markup(
    f"https://hackme.tech/news.html#{fid}",
    site_home="https://hackme.tech",
    extra_row=True,
    item_links=item.get("links") if isinstance(item.get("links"), dict) else None,
    show_github=bool(int("${NEWS_SHOW_GITHUB_BUTTON:-0}")),
)
print(msg)
print("--- inline_keyboard ---")
print(json.dumps(btn, indent=2, ensure_ascii=False))
PY
