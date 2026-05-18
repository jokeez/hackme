#!/usr/bin/env bash
# One command: preflight tests + baseline + background overnight monitor until ~08:00.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"
chmod +x "$ROOT/scripts/ops/desktop_overnight_preflight.sh" \
  "$ROOT/scripts/ops/desktop_overnight_monitor.sh" \
  "$ROOT/scripts/ops/desktop_morning_report.sh"

echo "[overnight-start] preflight (tests + worker)"
bash "$ROOT/scripts/ops/desktop_overnight_preflight.sh"

RUN_ID="overnight_$(date -u +%Y%m%dT%H%M%SZ)"
OUT="$ROOT/reports/overnight/$RUN_ID"
mkdir -p "$OUT"
PID_FILE="$OUT/monitor.pid"

if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
  echo "[overnight-start] monitor already running pid=$(cat "$PID_FILE")"
  exit 0
fi

END_EPOCH="$(date -d 'tomorrow 08:00' +%s 2>/dev/null || date -d '+8 hours' +%s)"
echo "[overnight-start] monitor until $(date -d "@$END_EPOCH" 2>/dev/null || date -r "$END_EPOCH") (epoch=$END_EPOCH)"

nohup env RUN_ID="$RUN_ID" END_EPOCH="$END_EPOCH" INTERVAL_SEC="${INTERVAL_SEC:-60}" \
  bash "$ROOT/scripts/ops/desktop_overnight_monitor.sh" \
  >"$OUT/nohup.out" 2>&1 &
echo $! >"$PID_FILE"
ln -sfn "$OUT" "$ROOT/reports/overnight/CURRENT"

echo "[overnight-start] RUN_ID=$RUN_ID pid=$(cat "$PID_FILE")"
echo "[overnight-start] tail: tail -f $OUT/monitor.log"
echo "[morning] bash scripts/ops/desktop_morning_report.sh"
