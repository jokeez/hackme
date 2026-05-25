#!/usr/bin/env bash
# Set TG_ADMIN_CHAT_ID on VPS news bot env (operator heartbeat). Do not commit chat id to git.
#
# Usage:
#   TG_ADMIN_CHAT_ID=1003964720911 NODE_SSH=hackme-vps bash scripts/ops/vps_set_telegram_admin_chat.sh
#
set -euo pipefail
NODE_SSH="${NODE_SSH:-hackme-vps}"
ADMIN_ID="${TG_ADMIN_CHAT_ID:-${1:-}}"
if [[ -z "$ADMIN_ID" ]]; then
  echo "[tg-admin] set TG_ADMIN_CHAT_ID or pass as first arg" >&2
  exit 1
fi
ssh -o BatchMode=yes "$NODE_SSH" "ADMIN_ID='$ADMIN_ID' bash -s" <<'REMOTE'
set -euo pipefail
ENV=/opt/hackme/.env.newsbot
if [[ ! -f "$ENV" ]]; then
  cp /opt/hackme/scripts/ops/telegram/newsbot.env.example "$ENV"
  chmod 600 "$ENV"
fi
if grep -q '^TG_ADMIN_CHAT_ID=' "$ENV"; then
  sudo sed -i "s|^TG_ADMIN_CHAT_ID=.*|TG_ADMIN_CHAT_ID=${ADMIN_ID}|" "$ENV"
else
  echo "TG_ADMIN_CHAT_ID=${ADMIN_ID}" | sudo tee -a "$ENV" >/dev/null
fi
sudo chmod 600 "$ENV"
grep '^TG_ADMIN_CHAT_ID=' "$ENV" | sed 's/=.*/=***set***/'
if systemctl is-active hackme-news-bot.service >/dev/null 2>&1; then
  sudo systemctl restart hackme-news-bot.service
  echo "[tg-admin] hackme-news-bot restarted"
else
  echo "[tg-admin] hackme-news-bot not active — run setup_news_bot_service.sh on VPS"
fi
REMOTE
echo "[tg-admin] done"
