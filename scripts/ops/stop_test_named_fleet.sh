#!/usr/bin/env bash
# Stop test named hybrid fleet (PoH+fuzz). Never touches worker-kapa-pc.
# Prefer systemctl only — do not pkill patterns that can match the controlling shell.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT/logs/test-named-fleet}"
UNIT_PREFIX="${UNIT_PREFIX:-hackme-test-poh}"

while IFS= read -r unit; do
  [[ -n "$unit" ]] || continue
  systemctl --user stop "$unit" 2>/dev/null || true
  systemctl --user reset-failed "$unit" 2>/dev/null || true
done < <(systemctl --user list-units --all --type=service --no-legend "${UNIT_PREFIX}-*" 2>/dev/null | awk '{print $1}')

# Also stop any leftover separate fuzz-only units.
if [[ -x "$ROOT/scripts/ops/stop_test_named_fuzz_fleet.sh" ]]; then
  bash "$ROOT/scripts/ops/stop_test_named_fuzz_fleet.sh" >/dev/null 2>&1 || true
fi

if [[ -d "$LOG_DIR" ]]; then
  rm -f "$LOG_DIR"/*.pid "$LOG_DIR"/*.unit 2>/dev/null || true
fi

sleep 1
alive="$(systemctl --user list-units --type=service --state=running "${UNIT_PREFIX}-*" --no-legend 2>/dev/null | wc -l | tr -d ' ')"
echo "[test-fleet] stopped hybrid PoH+fuzz (remaining units=${alive}; kapa-pc untouched)"
