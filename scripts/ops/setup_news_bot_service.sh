#!/usr/bin/env bash
set -euo pipefail

# Install and start HackMe Telegram News Bot systemd service on VPS.
#
# Usage:
#   sudo bash scripts/ops/setup_news_bot_service.sh
#
# Pre-req:
#   - /opt/hackme exists (deployed repo)
#   - /opt/hackme/.env.newsbot exists and contains TG_BOT_TOKEN + TG_CHAT_ID

SERVICE_SRC="/opt/hackme/scripts/ops/systemd/hackme-news-bot.service"
SERVICE_DST="/etc/systemd/system/hackme-news-bot.service"
ENV_FILE="/opt/hackme/.env.newsbot"

if [[ ! -f "$SERVICE_SRC" ]]; then
  echo "[news-bot-setup] missing service file: $SERVICE_SRC" >&2
  exit 1
fi
if [[ ! -f "$ENV_FILE" ]]; then
  echo "[news-bot-setup] missing env file: $ENV_FILE" >&2
  echo "Create it from /opt/hackme/scripts/ops/telegram/newsbot.env.example" >&2
  exit 1
fi

mkdir -p /opt/hackme/data /opt/hackme/logs
chmod 700 /opt/hackme/data /opt/hackme/logs
chmod 600 "$ENV_FILE"

install -m 0644 "$SERVICE_SRC" "$SERVICE_DST"
systemctl daemon-reload
systemctl enable --now hackme-news-bot.service
sleep 1
systemctl --no-pager --full status hackme-news-bot.service || true

echo
echo "[news-bot-setup] tail logs:"
echo "  journalctl -u hackme-news-bot.service -n 80 --no-pager"
echo "  tail -n 80 /opt/hackme/logs/news-bot.log"
