#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT_DIR/logs/desktop}"
PID_FILE="$LOG_DIR/node.pid"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"

if [[ -f "$PID_FILE" ]]; then
  pid="$(cat "$PID_FILE" 2>/dev/null || true)"
else
  pid=""
fi

running="0"
if [[ -n "$pid" ]] && kill -0 "$pid" >/dev/null 2>&1; then
  running="1"
fi

health="down"
status_json=""
if status_json="$(curl -fsS --max-time 2 "$BASE_URL/api/status" 2>/dev/null)"; then
  health="up"
fi

echo "[desktop-status] running=$running pid=${pid:-none} health=$health base=$BASE_URL"
if [[ "$health" == "up" && -n "$status_json" ]]; then
  python3 - "$status_json" <<'PY'
import json,sys
raw=sys.argv[1]
try:
    j=json.loads(raw)
    print("[desktop-status] tip_height=%s mining=%s node=%s" % (j.get("tip_height"), j.get("mining"), j.get("node_address")))
except Exception:
    print("[desktop-status] warning: /api/status returned non-json payload")
PY
fi
