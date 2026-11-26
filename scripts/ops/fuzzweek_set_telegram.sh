#!/usr/bin/env bash
# Point the Free Fuzz Week intake at a Telegram bot, then prove it works.
#
# The token is a secret: it is read from the environment or prompted for silently,
# written to a 0640 env file, and never echoed, logged or passed on a command line
# where `ps` could see it.
#
# Usage, on the host that runs the intake:
#   FUZZWEEK_TG_TOKEN=... FUZZWEEK_TG_CHAT=... sudo -E bash fuzzweek_set_telegram.sh
# or simply:
#   sudo bash fuzzweek_set_telegram.sh        # prompts for both
set -euo pipefail

ENV_FILE="${FUZZWEEK_ENV_FILE:-/opt/hackme/.env.fuzzweek}"
UNIT="${FUZZWEEK_UNIT:-hackme-fuzzweek-intake.service}"

token="${FUZZWEEK_TG_TOKEN:-}"
chat="${FUZZWEEK_TG_CHAT:-}"

if [ -z "$token" ]; then
  read -r -s -p "Telegram bot token (input hidden): " token; echo
fi
if [ -z "$chat" ]; then
  read -r -p "Operator chat id: " chat
fi
if [ -z "$token" ] || [ -z "$chat" ]; then
  echo "[fuzzweek-tg] both a token and a chat id are required" >&2
  exit 2
fi
case "$chat" in
  ''|*[!0-9-]*) echo "[fuzzweek-tg] chat id must be numeric (it may start with -)" >&2; exit 2 ;;
esac
if [ ! -f "$ENV_FILE" ]; then
  echo "[fuzzweek-tg] $ENV_FILE not found — is the intake installed?" >&2
  exit 1
fi

# Verify the credentials BEFORE writing them, so a typo cannot leave the service
# configured with something that does not work.
echo "[fuzzweek-tg] checking the bot and chat…"
# Everything, the token included, goes to curl through a stdin config file: putting the
# URL in argv would expose the token to any user running ps while the request is open.
resp="$(printf '%s\n' \
  "url = \"https://api.telegram.org/bot${token}/sendMessage\"" \
  'data-urlencode = "chat_id='"${chat}"'"' \
  'data-urlencode = "text=HackMe Free Fuzz Week intake is now wired to this chat. Submissions will arrive here."' \
  'data-urlencode = "disable_web_page_preview=true"' \
  'silent' \
  'max-time = 15' \
  | curl --config - || true)"
case "$resp" in
  *'"ok":true'*) echo "[fuzzweek-tg] test message delivered" ;;
  *)
    # Telegram echoes the request, not the token, in its error body; still, print only
    # the description field rather than the whole response.
    desc="$(printf '%s' "$resp" | sed -n 's/.*"description":"\([^"]*\)".*/\1/p')"
    echo "[fuzzweek-tg] Telegram refused: ${desc:-no response}" >&2
    echo "[fuzzweek-tg] nothing was written; fix the token or chat id and run again" >&2
    exit 1
    ;;
esac

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
grep -vE '^(HACKME_TELEGRAM_BOT_TOKEN|HACKME_TELEGRAM_CHAT_ID)=' "$ENV_FILE" > "$tmp"
printf 'HACKME_TELEGRAM_BOT_TOKEN=%s\n' "$token" >> "$tmp"
printf 'HACKME_TELEGRAM_CHAT_ID=%s\n' "$chat" >> "$tmp"
install -o hackme -g hackme -m 0640 "$tmp" "$ENV_FILE"
echo "[fuzzweek-tg] wrote credentials to $ENV_FILE (0640, not printed)"

systemctl restart "$UNIT"
sleep 2
systemctl is-active --quiet "$UNIT" || { echo "[fuzzweek-tg] $UNIT failed to restart" >&2; exit 1; }
if journalctl -u "$UNIT" -n 20 --no-pager -o cat | grep -q "notifications enabled"; then
  echo "[fuzzweek-tg] intake restarted with notifications ENABLED"
else
  echo "[fuzzweek-tg] intake restarted, but it still reports notifications disabled" >&2
  exit 1
fi
echo "[fuzzweek-tg] done — submit a test on https://hackme.tech/fuzz-week.html to see it arrive"
