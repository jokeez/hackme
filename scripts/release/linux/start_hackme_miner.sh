#!/usr/bin/env bash
# Public-pool miner — extract tarball, run this script. No manual token or pool URL.
set -euo pipefail

INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$INSTALL_DIR"

ENV_FILE="${ENV_FILE:-$INSTALL_DIR/.env}"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"
LOG_DIR="${LOG_DIR:-$INSTALL_DIR/logs}"
PID_FILE="$LOG_DIR/hackme-node.pid"
NODE_LOG="$LOG_DIR/hackme-node.log"

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "[miner] missing command: $1" >&2
    exit 1
  }
}

require_cmd curl

if [[ ! -x "$INSTALL_DIR/hackme" ]]; then
  echo "[miner] hackme binary missing — extract the linux/ folder from the release tarball" >&2
  exit 1
fi

if [[ ! -f "$ENV_FILE" ]]; then
  echo "[miner] first run — configuring pool access..."
  bash "$INSTALL_DIR/setup_hackme_miner.sh"
fi

if [[ ! -f "$INSTALL_DIR/pool.miner.token" ]]; then
  echo "[miner] pool.miner.token missing — download from https://hackme.tech/downloads.html" >&2
  exit 1
fi

set -a
# shellcheck disable=SC1090
. "$ENV_FILE"
set +a

if [[ -f "$LOG_DIR/mining_paused" ]]; then
  echo "[miner] mining paused — run: bash stop_hackme_miner.sh (clear) or rm $LOG_DIR/mining_paused" >&2
  echo "[miner] to resume: bash start_hackme_miner.sh after clearing pause" >&2
  exit 0
fi

if [[ -z "${HACKME_POOL_COORDINATOR_TOKEN:-}" ]]; then
  export HACKME_POOL_COORDINATOR_TOKEN="$(tr -d '\r\n' <"$INSTALL_DIR/pool.miner.token")"
fi
export HACKME_DATA_DIR="${HACKME_DATA_DIR:-$INSTALL_DIR/data}"
export HACKME_DESKTOP_MODE="${HACKME_DESKTOP_MODE:-1}"
export HACKME_WORKER_WATCHDOG="${HACKME_WORKER_WATCHDOG:-1}"
mkdir -p "$LOG_DIR" "$HACKME_DATA_DIR"

stop_node() {
  if [[ -f "$PID_FILE" ]]; then
    local old_pid
    old_pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
      kill "$old_pid" 2>/dev/null || true
      sleep 1
    fi
    rm -f "$PID_FILE"
  fi
}

if [[ -f "$PID_FILE" ]]; then
  old_pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
    if curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
      echo "[miner] node already running (pid=$old_pid) — $BASE_URL"
    else
      stop_node
    fi
  else
    rm -f "$PID_FILE"
  fi
fi

start_worker() {
  local coord_url
  coord_url="$(curl -fsS "$BASE_URL/api/status" 2>/dev/null | python3 -c '
import json,sys
d=json.load(sys.stdin)
print((d.get("pool_coordinator_url_effective") or d.get("pool_coordinator_url") or "").strip())
' 2>/dev/null || true)"
  [[ -n "$coord_url" ]] || coord_url="https://hackme.tech/pool/coordinator"
  curl -fsS -X POST "$BASE_URL/api/worker/start" \
    -H "Content-Type: application/json" \
    -H "X-Hackme-Admin-Token: ${HACKME_ADMIN_TOKEN}" \
    -d "{\"coord_url\":\"${coord_url}\"}" >/dev/null 2>&1 || true
}

if [[ ! -f "$PID_FILE" ]]; then
  echo "[miner] starting HackMe node (pool: hackme.tech)..."
  nohup "$INSTALL_DIR/hackme" >"$NODE_LOG" 2>&1 &
  echo "$!" >"$PID_FILE"
fi

for _ in $(seq 1 60); do
  if curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

if ! curl -fsS "$BASE_URL/api/status" >/dev/null 2>&1; then
  echo "[miner] node did not start — see $NODE_LOG" >&2
  tail -n 30 "$NODE_LOG" 2>/dev/null || true
  exit 1
fi

start_worker

if command -v xdg-open >/dev/null 2>&1; then
  (sleep 2; xdg-open "$BASE_URL/#ecosystem" >/dev/null 2>&1 &) || true
fi

echo ""
echo "HackMe miner is running."
echo "  Dashboard: $BASE_URL"
echo "  Logs:      $NODE_LOG"
echo "  Stop:      kill \$(cat $PID_FILE)"
echo ""

# Default: daemon mode — closing the terminal must not kill mining.
# Opt into foreground follow with: HACKME_MINER_DAEMON=0 bash start_hackme_miner.sh
if [[ "${HACKME_MINER_DAEMON:-1}" == "1" ]]; then
  echo "Running in background (HACKME_MINER_DAEMON=1). Follow logs: tail -f $NODE_LOG"
  exit 0
fi

echo "Keep this terminal open, or run: tail -f $NODE_LOG"
echo ""
tail -f "$NODE_LOG"
