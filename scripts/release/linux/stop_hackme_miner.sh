#!/usr/bin/env bash
# Stop release tarball miner (node + workers + port 8080).
set -euo pipefail
INSTALL_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$INSTALL_DIR"

LOG_DIR="${LOG_DIR:-$INSTALL_DIR/logs}"
ENV_FILE="${ENV_FILE:-$INSTALL_DIR/.env}"
PID_FILE="$LOG_DIR/hackme-node.pid"
BASE_URL="${BASE_URL:-http://127.0.0.1:8080}"

if [[ -x "$INSTALL_DIR/scripts/ops/stop_pool_workers.sh" ]]; then
  LOG_DIR="$LOG_DIR" ROOT_DIR="$INSTALL_DIR" ENV_FILE="$ENV_FILE" \
    HACKME_MINING_PAUSED_FILE="$LOG_DIR/mining_paused" \
    bash "$INSTALL_DIR/scripts/ops/stop_pool_workers.sh" || true
elif [[ -x "$INSTALL_DIR/stop_pool_workers.sh" ]]; then
  LOG_DIR="$LOG_DIR" ROOT_DIR="$INSTALL_DIR" ENV_FILE="$ENV_FILE" \
    bash "$INSTALL_DIR/stop_pool_workers.sh" || true
fi

if [[ -f "$PID_FILE" ]]; then
  pid="$(cat "$PID_FILE" 2>/dev/null || true)"
  if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    sleep 0.5
    kill -9 "$pid" 2>/dev/null || true
  fi
  rm -f "$PID_FILE"
fi

port="${HACKME_BIND_ADDR##*:}"
port="${port:-8080}"
if command -v fuser >/dev/null 2>&1; then
  fuser -k -TERM "${port}/tcp" >/dev/null 2>&1 || true
fi

echo "[stop-miner] OK — paused. Resume: bash start_hackme_miner.sh (after rm $LOG_DIR/mining_paused or resume_pool_mining.sh)"
