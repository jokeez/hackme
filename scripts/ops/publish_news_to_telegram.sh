#!/usr/bin/env bash
# Publish new items from hackme.tech/assets/news.json to the Telegram channel bot.
# Usage:
#   NODE_SSH=hackme-vps bash scripts/ops/publish_news_to_telegram.sh
#   FORCE_NEWS_ID=2026-05-21-coordinator-stress-bct bash scripts/ops/publish_news_to_telegram.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/ops/_deploy_ssh_retry.sh
source "$ROOT/scripts/ops/_deploy_ssh_retry.sh"
NODE_SSH="${NODE_SSH:-hackme-vps}"
FORCE_ID="${FORCE_NEWS_ID:-}"

_deploy_ssh() {
  if [[ -n "${HACKME_DEPLOY_SSH_IDENTITY:-}" && -f "${HACKME_DEPLOY_SSH_IDENTITY}" ]]; then
    ssh -i "${HACKME_DEPLOY_SSH_IDENTITY}" -o IdentitiesOnly=yes -o BatchMode=yes -o StrictHostKeyChecking=accept-new "$@"
  else
    ssh -o BatchMode=yes "$@"
  fi
}

echo "[news-tg] verify live feed (from VPS — avoids CDN truncation on slow paths)"
_deploy_ssh "$NODE_SSH" "curl -fsS --max-time 20 -H 'Accept-Encoding: identity' 'https://hackme.tech/assets/news.json' | python3 -c \"
import json,sys
d=json.load(sys.stdin)
print('top:', d['items'][0]['id'], d['items'][0]['title'][:60])
\""

if [[ -n "$FORCE_ID" ]]; then
  echo "[news-tg] remove $FORCE_ID from posted_ids on $NODE_SSH (force republish)"
  _deploy_ssh "$NODE_SSH" "python3 - <<'PY'
import json, os
p='/opt/hackme/data/news-bot-state.json'
fid='${FORCE_ID}'
if os.path.isfile(p):
    s=json.load(open(p))
    s['posted_ids']=[x for x in s.get('posted_ids',[]) if x!=fid]
    # Allow same body after explicit FORCE (fingerprints would otherwise block).
    s['posted_fingerprints']=[]
    with open(p,'w',encoding='utf-8') as f:
        json.dump(s, f, indent=2)
        f.write('\\n')
    print('updated', p)
else:
    print('no state file yet')
PY"
fi

echo "[news-tg] run channel bot --once (flock serializes vs daemon)"
_deploy_ssh "$NODE_SSH" 'cd /opt/hackme && set -a && [ -f /opt/hackme/.env.newsbot ] && . /opt/hackme/.env.newsbot; set +a; export NEWS_SHOW_GITHUB_BUTTON="${NEWS_SHOW_GITHUB_BUTTON:-0}"; export MAX_POSTS_PER_CYCLE="${MAX_POSTS_PER_CYCLE:-1}"; python3 scripts/ops/telegram/news_channel_bot.py --once' 2>&1
