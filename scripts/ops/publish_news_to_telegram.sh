#!/usr/bin/env bash
# Publish new items from hackme.tech/assets/news.json to the Telegram channel bot.
# Usage:
#   NODE_SSH=hackme-vps bash scripts/ops/publish_news_to_telegram.sh
#   FORCE_NEWS_ID=2026-05-21-coordinator-stress-bct bash scripts/ops/publish_news_to_telegram.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
FORCE_ID="${FORCE_NEWS_ID:-}"

echo "[news-tg] verify live feed"
curl -fsS --max-time 45 "https://hackme.tech/assets/news.json" | python3 -c "
import json,sys
d=json.load(sys.stdin)
print('top:', d['items'][0]['id'], d['items'][0]['title'][:60])
"

if [[ -n "$FORCE_ID" ]]; then
  echo "[news-tg] remove $FORCE_ID from posted_ids on $NODE_SSH (force republish)"
  ssh -o BatchMode=yes "$NODE_SSH" "python3 - <<'PY'
import json, os
p='/opt/hackme/data/news-bot-state.json'
fid='${FORCE_ID}'
if os.path.isfile(p):
    s=json.load(open(p))
    s['posted_ids']=[x for x in s.get('posted_ids',[]) if x!=fid]
    json.dump(s, open(p,'w'), indent=2)
    print('updated', p)
else:
    print('no state file yet')
PY"
fi

echo "[news-tg] run channel bot --once"
ssh -o BatchMode=yes "$NODE_SSH" 'cd /opt/hackme && set -a && [ -f /opt/hackme/.env.newsbot ] && . /opt/hackme/.env.newsbot; set +a; python3 scripts/ops/telegram/news_channel_bot.py --once' 2>&1
