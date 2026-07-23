#!/usr/bin/env bash
set -euo pipefail

# Lightweight watchdog for hackme-news-bot.service.
# Intended for timer-based execution (every minute).
# Quiet on healthy path — only log when restarting or failing.

SERVICE_NAME="${SERVICE_NAME:-hackme-news-bot.service}"
LOG_FILE="${LOG_FILE:-/opt/hackme/logs/news-bot-watchdog.log}"

mkdir -p "$(dirname "$LOG_FILE")"

ts() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }

if systemctl is-active --quiet "$SERVICE_NAME"; then
  exit 0
fi

echo "$(ts) [watchdog] ${SERVICE_NAME} inactive -> restart" >>"$LOG_FILE"
systemctl restart "$SERVICE_NAME"
sleep 1

if systemctl is-active --quiet "$SERVICE_NAME"; then
  echo "$(ts) [watchdog] restart success" >>"$LOG_FILE"
  exit 0
fi

echo "$(ts) [watchdog] restart failed" >>"$LOG_FILE"
exit 1
