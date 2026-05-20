#!/usr/bin/env bash
# 3-hour mining snapshot watch (default). Usage: bash scripts/ops/start_mining_watch.sh [hours]
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
HOURS="${1:-3}"
RUN_ID="${RUN_ID:-watch_$(date -u +%Y%m%dT%H%M%SZ)}"
END_EPOCH="$(date -d "+${HOURS} hours" +%s 2>/dev/null || echo $(( $(date +%s) + HOURS * 3600 )))"
export RUN_ID END_EPOCH INTERVAL_SEC="${INTERVAL_SEC:-60}"

bash "$ROOT/scripts/ops/mining_night_baseline.sh" "$RUN_ID"
pkill -f "desktop_overnight_monitor.sh.*${RUN_ID}" 2>/dev/null || true
sleep 1
mkdir -p "$ROOT/reports/overnight/$RUN_ID"
nohup bash "$ROOT/scripts/ops/desktop_overnight_monitor.sh" >>"$ROOT/reports/overnight/$RUN_ID/monitor.nohup.log" 2>&1 &
echo $! >"$ROOT/reports/overnight/$RUN_ID/monitor.pid"
ln -sfn "$ROOT/reports/overnight/$RUN_ID" "$ROOT/reports/overnight/CURRENT"

echo "[watch] run_id=$RUN_ID duration=${HOURS}h end_epoch=$END_EPOCH"
echo "[watch] folder: $ROOT/reports/overnight/$RUN_ID"
echo "[watch] check: bash scripts/ops/mining_morning_check.sh $RUN_ID"
