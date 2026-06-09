#!/usr/bin/env bash
set -euo pipefail

# HTTP watchdog for hackme-node.service (systemd active != responsive).
# Intended for timer-based execution (every minute).

SERVICE_NAME="${SERVICE_NAME:-hackme-node.service}"
CHAIN_BASE="${CHAIN_BASE:-http://127.0.0.1:18080}"
STATUS_URL="${STATUS_URL:-${CHAIN_BASE}/api/status}"
CURL_MAX_SEC="${CURL_MAX_SEC:-8}"
LOG_FILE="${LOG_FILE:-/opt/hackme/logs/node-watchdog.log}"

mkdir -p "$(dirname "$LOG_FILE")"

ts() { date -u +"%Y-%m-%dT%H:%M:%SZ"; }

if curl -fsS --max-time "$CURL_MAX_SEC" "$STATUS_URL" | jq -e '.tip_height >= 0' >/dev/null 2>&1; then
  echo "$(ts) [node-watchdog] ${STATUS_URL} OK" >>"$LOG_FILE"
  exit 0
fi

echo "$(ts) [node-watchdog] ${STATUS_URL} failed or hung -> restart ${SERVICE_NAME}" >>"$LOG_FILE"
systemctl restart "$SERVICE_NAME"
sleep 2

if curl -fsS --max-time "$CURL_MAX_SEC" "$STATUS_URL" | jq -e '.tip_height >= 0' >/dev/null 2>&1; then
  echo "$(ts) [node-watchdog] restart success" >>"$LOG_FILE"
  exit 0
fi

echo "$(ts) [node-watchdog] restart failed — still unreachable" >>"$LOG_FILE"
exit 1
