#!/usr/bin/env bash
# Start wave15 pipeline (waits for wave14 asan-chase, then discovery + wave15/16).
#
#   bash scripts/ops/start_wave15_pipeline.sh
#   bash scripts/ops/start_wave15_pipeline.sh --now   # skip wait if wave14 already done
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
mkdir -p "$ROOT/logs"

if [[ "${1:-}" == "--now" ]]; then
  export SKIP_WAVE14_WAIT=1
fi

LOG="$ROOT/logs/bounty-wave15-pipeline.nohup.log"
echo "[start] $(date -u +%Y-%m-%dT%H:%M:%SZ) launching run_bounty_hunt_after_wave14.sh → $LOG"
setsid bash "$ROOT/scripts/ops/run_bounty_hunt_after_wave14.sh" >>"$LOG" 2>&1 &
echo $! >"$ROOT/logs/wave15-pipeline.pid"
echo "[start] pid=$(cat "$ROOT/logs/wave15-pipeline.pid") tail -f $LOG"
