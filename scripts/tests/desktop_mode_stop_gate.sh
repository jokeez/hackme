#!/usr/bin/env bash
# Gate: desktop_mode_stop pauses workers and frees :8080.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
# shellcheck source=scripts/tests/common.sh
source "$ROOT/scripts/tests/common.sh"

LOG_DIR="$ROOT/logs/desktop"
PAUSE="$LOG_DIR/mining_paused"
rm -f "$PAUSE" 2>/dev/null || true

# Simulate pause without killing real mining in CI — only test stop_pool_workers writes pause + env.
if [[ "${DESKTOP_STOP_LIVE:-0}" == "1" ]]; then
  bash "$ROOT/scripts/ops/desktop_mode_stop.sh" || fail "desktop_mode_stop failed"
  [[ -f "$PAUSE" ]] || fail "mining_paused file missing"
  if pgrep -af 'workerpoh' 2>/dev/null | grep -v desktop_mode_stop_gate; then
    fail "workerpoh still running after desktop_mode_stop"
  fi
  if ss -lntp 2>/dev/null | grep -q ':8080 '; then
    fail "port 8080 still bound after desktop_mode_stop"
  fi
else
  LOG_DIR="$LOG_DIR" ROOT_DIR="$ROOT" bash "$ROOT/scripts/ops/stop_pool_workers.sh" || true
  [[ -f "$PAUSE" ]] || [[ -f "$ROOT/logs/mining_paused" ]] || fail "pause file not created"
  rm -f "$PAUSE" "$ROOT/logs/mining_paused" 2>/dev/null || true
fi

pass "desktop mode stop gate PASS"
