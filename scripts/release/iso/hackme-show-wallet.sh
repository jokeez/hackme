#!/usr/bin/env bash
# Re-display payout wallet (recovery phrase only if still on this rig).
set -euo pipefail

ENV_STATE="/var/lib/hackme/miner.env"
INI="/var/lib/hackme/hackme.ini"
[[ -f "$ENV_STATE" ]] && source "$ENV_STATE"

WALLET="${HACKME_PAYOUT_WALLET:-}"
if [[ -z "$WALLET" && -f "$INI" ]]; then
  WALLET="$(grep -E '^wallet=' "$INI" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '[:space:]')"
fi

echo "=== HackMe OS wallet ==="
if [[ -n "$WALLET" ]]; then
  echo "Payout address: $WALLET"
else
  echo "Payout address: (not set — reboot for Zero-Knowledge Start or edit $INI)"
fi
if [[ -f /run/hackme-os/zk-wallet.json ]]; then
  echo ""
  echo "Recovery phrase (this boot):"
  jq -r '.recovery_phrase // empty' /run/hackme-os/zk-wallet.json 2>/dev/null | fold -s -w 72
fi
if [[ -f /var/lib/hackme/recovery.phrase ]]; then
  echo ""
  echo "Recovery phrase (saved on rig):"
  cat /var/lib/hackme/recovery.phrase
  echo ""
  echo "WARNING: phrase on disk — copy to paper and delete this file on shared rigs."
fi
