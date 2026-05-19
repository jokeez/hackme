#!/usr/bin/env bash
# Deploy HackMe operator Telegram bot (hashrate / blocks / pool / tasks — NOT news channel).
#
# Prereq on VPS: /opt/hackme/.env.telegram with:
#   TELEGRAM_BOT_TOKEN=...          (from @BotFather — separate bot from news channel)
#   HACKME_TELEGRAM_NODE_URL=http://127.0.0.1:18080
#   HACKME_TELEGRAM_ALLOWED_USER_IDS=your_numeric_id   (recommended)
#
#   NODE_SSH=hackme-vps bash scripts/ops/setup_telegram_operator_bot.sh
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
NODE_SSH="${NODE_SSH:-hackme-vps}"
DEPLOY="${NODE_DEPLOY_DIR:-/opt/hackme}"

echo "[tg-op] build hackme-telegrambot"
(cd "$ROOT" && go build -o /tmp/hackme-telegrambot ./cmd/telegrambot/)

echo "[tg-op] rsync binary + systemd unit"
rsync -avz /tmp/hackme-telegrambot \
  "$ROOT/scripts/ops/systemd/hackme-telegrambot.service" \
  "$ROOT/scripts/ops/telegram_bot.env.example" \
  "$NODE_SSH:$DEPLOY/"
rsync -avz "$ROOT/scripts/ops/systemd/hackme-telegrambot.service" "$NODE_SSH:/tmp/"

ssh -o BatchMode=yes "$NODE_SSH" "bash -s" <<REMOTE
set -euo pipefail
DEPLOY='$DEPLOY'
if [[ ! -f "\$DEPLOY/.env.telegram" ]]; then
  cp "\$DEPLOY/telegram_bot.env.example" "\$DEPLOY/.env.telegram"
  sed -i 's|HACKME_TELEGRAM_NODE_URL=.*|HACKME_TELEGRAM_NODE_URL=http://127.0.0.1:18080|' "\$DEPLOY/.env.telegram"
  chmod 600 "\$DEPLOY/.env.telegram"
  echo "[tg-op] WARN: created \$DEPLOY/.env.telegram — set TELEGRAM_BOT_TOKEN and ALLOWED_USER_IDS, then re-run" >&2
  exit 2
fi
grep -q '^TELEGRAM_BOT_TOKEN=.\+' "\$DEPLOY/.env.telegram" || {
  echo "[tg-op] FATAL: TELEGRAM_BOT_TOKEN empty in .env.telegram" >&2
  exit 2
}
sudo cp /tmp/hackme-telegrambot.service /etc/systemd/system/
sudo chmod 755 "\$DEPLOY/hackme-telegrambot"
sudo systemctl daemon-reload
sudo systemctl enable hackme-telegrambot.service
sudo systemctl restart hackme-telegrambot.service
sleep 2
systemctl is-active hackme-telegrambot.service
journalctl -u hackme-telegrambot -n 5 --no-pager
REMOTE

echo "[tg-op] done — open your bot in Telegram and send /start or /pool"
