#!/usr/bin/env bash
# Re-display payout wallet + recovery phrase (structured terminal UI).
set -euo pipefail

UI="/opt/hackme/scripts/release/iso/hackme-os-ui.sh"
[[ -f "$UI" ]] && source "$UI"

ENV_STATE="/var/lib/hackme/miner.env"
INI="/var/lib/hackme/hackme.ini"
[[ -f "$ENV_STATE" ]] && source "$ENV_STATE"

WALLET="${HACKME_PAYOUT_WALLET:-}"
PHRASE=""
if [[ -z "$WALLET" && -f "$INI" ]]; then
  WALLET="$(grep -E '^wallet=' "$INI" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '[:space:]')"
fi
if [[ -f /run/hackme-os/zk-wallet.json ]]; then
  PHRASE="$(jq -r '.recovery_phrase // empty' /run/hackme-os/zk-wallet.json 2>/dev/null || true)"
fi
if [[ -z "$PHRASE" && -f /var/lib/hackme/recovery.phrase ]]; then
  PHRASE="$(tr '\n' ' ' </var/lib/hackme/recovery.phrase)"
fi

if declare -f hackme_ui_wallet_dashboard >/dev/null 2>&1; then
  hackme_ui_wallet_dashboard "$WALLET" "$PHRASE"
else
  echo "=== HackMe OS wallet ==="
  echo "Payout: ${WALLET:-(not set)}"
  [[ -n "$PHRASE" ]] && echo "Phrase: $PHRASE"
fi
