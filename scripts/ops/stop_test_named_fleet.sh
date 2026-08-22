#!/usr/bin/env bash
# Stop test named PoH fleet (never touches worker-kapa-pc).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT/logs/test-named-fleet}"
UNIT_PREFIX="${UNIT_PREFIX:-hackme-test-poh}"

while IFS= read -r unit; do
  [[ -n "$unit" ]] || continue
  systemctl --user stop "$unit" 2>/dev/null || true
  systemctl --user reset-failed "$unit" 2>/dev/null || true
done < <(systemctl --user list-units --all --type=service --no-legend "${UNIT_PREFIX}-*" 2>/dev/null | awk '{print $1}')

if [[ -d "$LOG_DIR" ]]; then
  for p in "$LOG_DIR"/*.pid; do
    [[ -f "$p" ]] || continue
    pid="$(tr -d '\r\n' <"$p" || true)"
    if [[ -n "${pid:-}" ]]; then
      kill -TERM "$pid" 2>/dev/null || true
      pkill -P "$pid" 2>/dev/null || true
    fi
    rm -f "$p"
  done
  rm -f "$LOG_DIR"/*.unit 2>/dev/null || true
fi

pkill -f 'logs/test-named-fleet/' 2>/dev/null || true
sleep 1
echo "[test-fleet] stopped (kapa-pc untouched)"
pgrep -af 'workerpoh.*worker-kapa-pc' | head -2 || true
