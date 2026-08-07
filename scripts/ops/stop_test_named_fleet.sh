#!/usr/bin/env bash
# Stop test named PoH fleet (never touches worker-kapa-pc).
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT/logs/test-named-fleet}"
UNIT_PREFIX="${UNIT_PREFIX:-hackme-test-poh}"

NAMES=(
  ashwood blackout coldline digsite eastwind
  faraday graphite harbour ironclad jackknife
  keystone lantern mercury northstar overdrive
  redline skyhook timber vault waypoint
  nova orbit ember flare blaze spark ridge pulse
  aurora comet drift echo frost gleam hawk
  nebula quark raven storm titan vortex
)

for name in "${NAMES[@]}"; do
  unit="${UNIT_PREFIX}-${name}"
  systemctl --user stop "${unit}.service" 2>/dev/null || true
  systemctl --user reset-failed "${unit}.service" 2>/dev/null || true
done

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
