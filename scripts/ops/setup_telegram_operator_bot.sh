#!/usr/bin/env bash
# Deploy HackMe operator Telegram bot (hashrate / blocks / pool / tasks — NOT news channel).
#
# Prereq: operator bot token from @BotFather (separate from news channel bot).
#   echo 'YOUR_TOKEN' > .secrets/telegram_operator_bot_token   # local, gitignored
#   # or on VPS: /opt/hackme/.secrets/telegram_operator_bot_token
#   # or: TELEGRAM_BOT_TOKEN=... when running this script
#
# Optional: HACKME_TELEGRAM_ALLOWED_USER_IDS=123456789 (comma-separated; strongly recommended)
#
#   NODE_SSH=hackme-vps bash scripts/ops/setup_telegram_operator_bot.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"

# Local one-line token file (gitignored under .secrets/)
if [[ -z "${TELEGRAM_BOT_TOKEN:-}" && -f "$ROOT/.secrets/telegram_operator_bot_token" ]]; then
  TELEGRAM_BOT_TOKEN="$(head -n1 "$ROOT/.secrets/telegram_operator_bot_token" | tr -d '\r\n')"
  export TELEGRAM_BOT_TOKEN
fi

echo "[tg-op] build hackme-telegrambot"
(cd "$ROOT" && go build -trimpath -ldflags "-s -w" -o /tmp/hackme-telegrambot ./cmd/telegrambot/)

echo "[tg-op] rsync binary + scripts + systemd unit"
rsync -avz /tmp/hackme-telegrambot \
  "$ROOT/scripts/ops/telegram_bot.env.example" \
  "$NODE_SSH:$DEPLOY/"
rsync -avz "$ROOT/scripts/ops/bootstrap_telegram_operator_env.sh" \
  "$NODE_SSH:$DEPLOY/scripts/ops/"
rsync -avz "$ROOT/scripts/ops/systemd/hackme-telegrambot.service" "$NODE_SSH:/tmp/"
# Optional: push local token file to VPS secrets (never commit)
if [[ -f "$ROOT/.secrets/telegram_operator_bot_token" ]]; then
  ssh -o BatchMode=yes "$NODE_SSH" "mkdir -p '$DEPLOY/.secrets' && chmod 700 '$DEPLOY/.secrets'"
  rsync -avz "$ROOT/.secrets/telegram_operator_bot_token" "$NODE_SSH:$DEPLOY/.secrets/"
fi

echo "[tg-op] bootstrap .env.telegram + enable systemd"
ssh -o BatchMode=yes "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
DEPLOY='$DEPLOY'
export TELEGRAM_BOT_TOKEN='${TELEGRAM_BOT_TOKEN:-}'
export HACKME_TELEGRAM_ALLOWED_USER_IDS='${HACKME_TELEGRAM_ALLOWED_USER_IDS:-}'
export HACKME_TELEGRAM_NODE_URL='${HACKME_TELEGRAM_NODE_URL:-http://127.0.0.1:18080}'
chmod +x "\$DEPLOY/scripts/ops/bootstrap_telegram_operator_env.sh"
bash "\$DEPLOY/scripts/ops/bootstrap_telegram_operator_env.sh"
sudo cp /tmp/hackme-telegrambot.service /etc/systemd/system/
sudo chmod 755 "\$DEPLOY/hackme-telegrambot"
sudo chown hackme:hackme "\$DEPLOY/hackme-telegrambot" "\$DEPLOY/.env.telegram" 2>/dev/null || true
mkdir -p "\$DEPLOY/logs"
sudo chown hackme:hackme "\$DEPLOY/logs" 2>/dev/null || true
sudo systemctl daemon-reload
sudo systemctl enable hackme-telegrambot.service
sudo systemctl restart hackme-telegrambot.service
sleep 2
systemctl is-active hackme-telegrambot.service
journalctl -u hackme-telegrambot -n 8 --no-pager
REMOTE

echo "[tg-op] done — open your operator bot in Telegram and send /start or /pool"
