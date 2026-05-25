#!/usr/bin/env bash
# Create or refresh $DEPLOY/.env.telegram (operator bot — hashrate/blocks/pool, NOT news channel).
#
# Bot token (first match):
#   TELEGRAM_BOT_TOKEN env, or $DEPLOY/.secrets/telegram_operator_bot_token (one line).
#
# HACKME_ADMIN_TOKEN copied from $DEPLOY/.env.vps when set.
#
# Usage on VPS:
#   DEPLOY=/opt/hackme bash /opt/hackme/scripts/ops/bootstrap_telegram_operator_env.sh
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DEPLOY="${DEPLOY:-$ROOT}"

_read_bot_token() {
  if [[ -n "${TELEGRAM_BOT_TOKEN:-}" ]]; then
    printf '%s' "$TELEGRAM_BOT_TOKEN"
    return 0
  fi
  local f="$DEPLOY/.secrets/telegram_operator_bot_token"
  if [[ -f "$f" ]]; then
    local t
    t="$(head -n1 "$f" | tr -d '\r\n' | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
    if [[ -n "$t" ]]; then
      printf '%s' "$t"
      return 0
    fi
  fi
  return 1
}

token=""
if ! token="$(_read_bot_token)"; then
  echo "[bootstrap-tg-op] missing TELEGRAM_BOT_TOKEN" >&2
  echo "  1) @BotFather → new bot (not the news-channel bot)" >&2
  echo "  2) echo 'TOKEN' | sudo tee $DEPLOY/.secrets/telegram_operator_bot_token && sudo chmod 600 $DEPLOY/.secrets/telegram_operator_bot_token" >&2
  echo "  3) re-run: NODE_SSH=hackme-vps bash scripts/ops/setup_telegram_operator_bot.sh" >&2
  exit 2
fi

admin_token=""
if [[ -f "$DEPLOY/.env.vps" ]]; then
  # shellcheck disable=SC1091
  set -a
  # shellcheck source=/dev/null
  source "$DEPLOY/.env.vps"
  set +a
  admin_token="${HACKME_ADMIN_TOKEN:-}"
fi

node_url="${HACKME_TELEGRAM_NODE_URL:-http://127.0.0.1:18080}"
allowed="${HACKME_TELEGRAM_ALLOWED_USER_IDS:-}"
watch_poll="${HACKME_TELEGRAM_WATCH_POLL_SEC:-45}"
env_file="$DEPLOY/.env.telegram"

mkdir -p "$DEPLOY/.secrets"
chmod 700 "$DEPLOY/.secrets" 2>/dev/null || true
umask 077
{
  echo "# HackMe operator Telegram bot — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "TELEGRAM_BOT_TOKEN=${token}"
  echo "HACKME_TELEGRAM_NODE_URL=${node_url}"
  echo "HACKME_TELEGRAM_WATCH_POLL_SEC=${watch_poll}"
  [[ -n "$allowed" ]] && echo "HACKME_TELEGRAM_ALLOWED_USER_IDS=${allowed}"
  [[ -n "$admin_token" ]] && echo "HACKME_ADMIN_TOKEN=${admin_token}"
} >"$env_file"
chmod 600 "$env_file"
chown hackme:hackme "$env_file" 2>/dev/null || true

echo "[bootstrap-tg-op] wrote $env_file (node=$node_url admin_token=$([ -n "$admin_token" ] && echo set || echo missing))"
