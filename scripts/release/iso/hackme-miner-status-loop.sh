#!/usr/bin/env bash
# Refresh status on tty1 without grabbing the controlling terminal (avoids black screen).
set -uo pipefail

STATUS="/opt/hackme/scripts/release/iso/hackme-miner-status.sh"
TTY="/dev/tty1"

while true; do
  if [[ -x "$STATUS" && -w "$TTY" ]]; then
    {
      printf '\033[H\033[2J'
      bash "$STATUS" 2>&1 || echo "[hackme-os] status script error"
      echo ""
      echo "— refresh in 30s · Alt+F2 tty2 · login root / hackme —"
    } >"$TTY" 2>/dev/null || true
  fi
  sleep 30
done
